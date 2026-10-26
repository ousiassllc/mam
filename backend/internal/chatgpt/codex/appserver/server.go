package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/rpc"
)

// State is where the app-server process stands.
type State string

// The states. Failed is a start that did not complete or an exit nobody asked for: the next use starts it again.
const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateFailed   State = "failed"
)

// Status is what the settings screen and the chat read of the process.
type Status struct {
	State State
	// Error says (for the user) why the state is Failed.
	Error string
}

// ErrStart means the process could not be started or did not complete the handshake. It is a failure that waiting may
// cure (the next call starts the process again).
var ErrStart = errors.New("codex app-server: could not start")

// ErrNotOurHome means the app-server reported a CODEX_HOME other than mam's own: mam would then share the user's
// credentials and settings, so the process is stopped.
var ErrNotOurHome = errors.New("codex app-server: it does not use mam's own CODEX_HOME")

// Options of a Server.
type Options struct {
	// Start starts a process; it is called on the first use and after each crash or stop.
	Start func() (Process, error)
	// CodexHome is the CODEX_HOME mam gave the process; the server must report the same directory.
	CodexHome string
	// Version is the mam version, sent as clientInfo.version.
	Version string
	Logger  *slog.Logger
	// InitTimeout bounds the handshake (default 30 s). StopTimeout is not needed: Process.Stop bounds itself.
	InitTimeout time.Duration
}

// Server is the one app-server process of mam, started on demand. It is safe for concurrent use.
type Server struct {
	opts Options

	listener atomic.Pointer[AccountListener]

	mu       sync.Mutex
	cur      *Session
	starting chan struct{} // closed when the start in progress ends
	status   Status
	closed   bool
	gen      int // bumped by Stop: a start that began before it is torn down
}

// New returns a Server with no process started.
func New(o Options) *Server {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.InitTimeout <= 0 {
		o.InitTimeout = 30 * time.Second
	}
	return &Server{opts: o, status: Status{State: StateStopped}}
}

// SetAccountListener sets the receiver of the account notifications (nil for none). It applies to the running process too.
func (s *Server) SetAccountListener(l AccountListener) {
	if l == nil {
		s.listener.Store(nil)
		return
	}
	s.listener.Store(&l)
}

func (s *Server) dispatchAccount(method string, params json.RawMessage) {
	if l := s.listener.Load(); l != nil {
		(*l)(method, params)
	}
}

// Status is the current state.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Session is a running, initialized process.
type Session struct {
	srv  *Server
	proc Process
	conn *rpc.Conn
	h    *handler
	// recorded is closed when the watcher has recorded the end of the process in the status (or found that it was
	// stopped, or replaced, and so records nothing).
	recorded chan struct{}
}

// Call sends a request to the process.
func (se *Session) Call(ctx context.Context, method string, params, result any) error {
	return se.conn.Call(ctx, method, params, result)
}

// Done is closed when the process ended.
func (se *Session) Done() <-chan struct{} { return se.conn.Done() }

// endWait is how long the exit status is waited for after the output of the process ended.
const endWait = 2 * time.Second

// recordWait is how long EndReason waits for the watcher to record the end: the watcher stops the process (which bounds
// itself) and waits for the exit status (endWait) first.
const recordWait = 10 * time.Second

// EndReason says, for the user, how the process ended: its exit code and the end of its standard error. A caller that
// learned of the end (the connection closed) gets it after the status says Failed (the watcher records it
// asynchronously), so what it reports and what the status shows agree.
func (se *Session) EndReason() string {
	select {
	case <-se.recorded:
	case <-time.After(recordWait):
	}
	return se.endReason()
}

// endReason is EndReason without waiting for the record: the watcher uses it to make the record. The output of a
// process ends just before it exits, so this waits a moment for the exit status.
func (se *Session) endReason() string {
	select {
	case <-se.proc.Done():
	case <-time.After(endWait):
	}
	if info := se.proc.ExitInfo(); info != "" {
		return info
	}
	if err := se.conn.Err(); err != nil {
		return err.Error()
	}
	return ""
}

// Subscribe starts the delivery of the thread's notifications. Call it before thread/start answers could be followed by a
// turn: the events of a thread that nobody subscribed to are dropped.
func (se *Session) Subscribe(threadID string) *Sub { return se.h.subscribe(threadID) }

// Unsubscribe ends the delivery for the thread.
func (se *Session) Unsubscribe(threadID string) { se.h.unsubscribe(threadID) }

// Ensure returns the running session, starting the process (and its handshake) when there is none. Callers that arrive
// during a start wait for it. A start that fails is the error (wrapping ErrStart) and the Failed status.
func (s *Server) Ensure(ctx context.Context) (*Session, error) {
	for {
		s.mu.Lock()
		switch {
		case s.closed:
			s.mu.Unlock()
			return nil, fmt.Errorf("%w: mam is shutting down", ErrStart)
		case s.cur != nil:
			cur := s.cur
			s.mu.Unlock()
			return cur, nil
		case s.starting != nil:
			wait := s.starting
			s.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		s.starting = make(chan struct{})
		s.status = Status{State: StateStarting}
		gen := s.gen
		s.mu.Unlock()
		return s.start(ctx, gen)
	}
}

func (s *Server) start(ctx context.Context, gen int) (*Session, error) {
	defer func() {
		s.mu.Lock()
		close(s.starting)
		s.starting = nil
		s.mu.Unlock()
	}()
	p, err := s.opts.Start()
	if err != nil {
		return nil, s.fail(fmt.Errorf("%w: %w", ErrStart, err))
	}
	h := newHandler(s.opts.Logger, s.dispatchAccount)
	conn := rpc.NewConn(p.Stdout(), p.Stdin(), h)
	se := &Session{srv: s, proc: p, conn: conn, h: h, recorded: make(chan struct{})}
	if err := s.handshake(ctx, se); err != nil {
		info := p.ExitInfo()
		p.Stop()
		conn.Close()
		if info != "" {
			err = fmt.Errorf("%w (%s)", err, info)
		}
		return nil, s.fail(err)
	}
	s.mu.Lock()
	if s.closed || s.gen != gen {
		s.mu.Unlock()
		p.Stop()
		conn.Close()
		return nil, s.stopped()
	}
	s.cur, s.status = se, Status{State: StateRunning}
	s.mu.Unlock()
	go s.watch(se)
	return se, nil
}

func (s *Server) handshake(ctx context.Context, se *Session) error {
	ctx, cancel := context.WithTimeout(ctx, s.opts.InitTimeout)
	defer cancel()
	var res protocol.InitializeResult
	err := se.conn.Call(ctx, protocol.MethodInitialize, protocol.InitializeParams{
		ClientInfo: protocol.ClientInfo{Name: protocol.ClientInfoName, Title: protocol.ClientInfoTitle, Version: s.opts.Version},
	}, &res)
	if err != nil {
		return fmt.Errorf("%w: initialize: %w", ErrStart, err)
	}
	if !sameDir(res.CodexHome, s.opts.CodexHome) {
		return fmt.Errorf("%w: %w", ErrStart, ErrNotOurHome)
	}
	if err := se.conn.Notify(protocol.NotifInitialized, struct{}{}); err != nil {
		return fmt.Errorf("%w: initialized: %w", ErrStart, err)
	}
	return nil
}

// sameDir tells whether two paths are the same directory. The server reports the CODEX_HOME it resolved (Codex makes
// the path absolute and follows symlinks), which can spell mam's directory differently, so equal spellings (ignoring
// case on Windows) are accepted first and else both are looked up on disk and compared with os.SameFile.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb || (runtime.GOOS == "windows" && strings.EqualFold(ca, cb)) {
		return true
	}
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}
