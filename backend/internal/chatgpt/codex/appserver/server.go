package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
}

// Call sends a request to the process.
func (se *Session) Call(ctx context.Context, method string, params, result any) error {
	return se.conn.Call(ctx, method, params, result)
}

// Done is closed when the process ended.
func (se *Session) Done() <-chan struct{} { return se.conn.Done() }

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
		s.mu.Unlock()
		return s.start(ctx)
	}
}

func (s *Server) start(ctx context.Context) (*Session, error) {
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
	se := &Session{srv: s, proc: p, conn: conn, h: h}
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
	if s.closed {
		s.mu.Unlock()
		p.Stop()
		return nil, fmt.Errorf("%w: mam is shutting down", ErrStart)
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

// sameDir compares two directories as paths, ignoring case on Windows.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// watch notices the end of the process: one nobody asked for is a failure (the status says so; the next use starts again).
func (s *Server) watch(se *Session) {
	select {
	case <-se.proc.Done():
	case <-se.conn.Done():
	}
	s.mu.Lock()
	if s.cur != se {
		s.mu.Unlock()
		return
	}
	s.cur = nil
	s.mu.Unlock()
	se.proc.Stop() // the connection may have ended with the process still running
	err := fmt.Errorf("%w", ErrStart)
	if info := se.proc.ExitInfo(); info != "" {
		err = errors.New(info)
	} else if e := se.conn.Err(); e != nil {
		err = e
	}
	s.opts.Logger.Warn("codex app-server ended", "state", "failed")
	_ = s.fail(err)
}

func (s *Server) fail(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.status = Status{State: StateFailed, Error: err.Error()}
	}
	return err
}

// Stop ends the process. The next use starts it again unless Close was called.
func (s *Server) Stop() {
	s.mu.Lock()
	se := s.cur
	s.cur = nil
	if se != nil || s.status.State == StateFailed {
		s.status = Status{State: StateStopped}
	}
	s.mu.Unlock()
	if se != nil {
		se.conn.Close()
		se.proc.Stop()
	}
}

// Close stops the process for good (mam is shutting down): later calls fail with ErrStart.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Stop()
}

// Running reports whether a process is up, without starting one.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur != nil
}
