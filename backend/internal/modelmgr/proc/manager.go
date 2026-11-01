// Package proc runs the bundled ComfyUI (and the local runtimes, spec/architecture/model-manager/local-runtime.md §5) as a child process of mam (spec/architecture/model-manager/model-manager.md §3.4): on
// 127.0.0.1 and a free port, without a console window, in a group that the operating system kills when mam ends or
// crashes (a Windows Job Object with KILL_ON_JOB_CLOSE; a process group and Pdeathsig on Linux), so that the files of
// the engine can be deleted by the uninstaller. Its state is a Status the screen and the chat read, and a start
// that fails or a crash is reported through Watch.
package proc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is where the child stands.
type State string

// The states of the child. Failed is a start that did not get ready or an exit nobody asked for; the next Start
// tries again.
const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateFailed   State = "failed"
)

// Status is what the child is doing.
type Status struct {
	State State  `json:"state"`
	Port  int    `json:"port,omitempty"`
	URL   string `json:"url,omitempty"`
	PID   int    `json:"pid,omitempty"`
	// Error says (for the user) why the state is Failed, with the end of the engine's log.
	Error     string    `json:"error,omitempty"`
	ExitCode  int       `json:"exit_code,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

// Spec is the command of one run.
type Spec struct {
	// Command and Args start the child in Dir. Env is added to the environment of mam.
	Command string
	Args    []string
	Dir     string
	Env     []string
	// DropEnv names variables of mam's own environment that the child must not inherit (compared without case on Windows).
	DropEnv []string
	// LogPath receives the output of the child ("" discards it).
	LogPath string
	// Name is the child's name in the log (default "comfyui"); Manager sets it from Config.Name.
	Name string
}

// Config of a Manager.
type Config struct {
	// Build returns the command for the port the child must listen on.
	Build func(port int) (Spec, error)
	// PreferredPort returns the port tried first (0 or nil: any free port), so that the address stays the same
	// from run to run.
	PreferredPort func() int
	// Ready returns nil once the child at url answers (default GET /system_stats is 200; HTTPReady makes another path).
	Ready func(ctx context.Context, url string) error
	// Name is what the messages call the child (default "ComfyUI"), for a runtime that is not ComfyUI.
	Name string
	// StartTimeout bounds the wait for Ready (default 3 minutes: the first start of the portable build loads
	// PyTorch). StopTimeout bounds the wait after the polite stop (default 10 seconds).
	StartTimeout time.Duration
	StopTimeout  time.Duration
	// PollEvery is the wait between two Ready probes (default 500 ms).
	PollEvery time.Duration
	Logger    *slog.Logger
}

// Manager runs at most one child.
type Manager struct {
	cfg Config

	mu       sync.Mutex
	status   Status
	cur      *run
	starting chan struct{} // closed when the start in progress ends
	watchers map[int]func(Status)
	nextID   int
}

// New returns a Manager. Build must be set.
func New(cfg Config) *Manager {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Ready == nil {
		cfg.Ready = HTTPReady("/system_stats")
	}
	if cfg.Name == "" {
		cfg.Name = "ComfyUI"
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 3 * time.Minute
	}
	if cfg.StopTimeout <= 0 {
		cfg.StopTimeout = 10 * time.Second
	}
	if cfg.PollEvery <= 0 {
		cfg.PollEvery = 500 * time.Millisecond
	}
	return &Manager{cfg: cfg, status: Status{State: StateStopped}, watchers: map[int]func(Status){}}
}

// Status is the current state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Watch calls fn with every change of the status (from any goroutine; fn must not block or call back into
// the Manager) until the returned function is called.
func (m *Manager) Watch(fn func(Status)) (cancel func()) {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.watchers[id] = fn
	m.mu.Unlock()
	return func() {
		m.mu.Lock()
		delete(m.watchers, id)
		m.mu.Unlock()
	}
}

// set stores s and tells the watchers. The caller holds no lock.
func (m *Manager) set(s Status) {
	m.mu.Lock()
	m.status = s
	fns := make([]func(Status), 0, len(m.watchers))
	for _, fn := range m.watchers {
		fns = append(fns, fn)
	}
	m.mu.Unlock()
	for _, fn := range fns {
		fn(s)
	}
}

// Start starts the child and waits until it answers. It returns the running Status at once when the child is
// running, and waits for the start in progress when there is one (two callers start one child). A failed start
// is the error and the Failed status.
func (m *Manager) Start(ctx context.Context) (Status, error) {
	for {
		m.mu.Lock()
		switch {
		case m.status.State == StateRunning:
			s := m.status
			m.mu.Unlock()
			return s, nil
		case m.starting != nil:
			wait := m.starting
			m.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return m.Status(), ctx.Err()
			}
		}
		m.starting = make(chan struct{})
		m.mu.Unlock()
		return m.startLocked(ctx)
	}
}

func (m *Manager) finishStart() {
	m.mu.Lock()
	close(m.starting)
	m.starting = nil
	m.mu.Unlock()
}

func (m *Manager) startLocked(ctx context.Context) (Status, error) {
	defer m.finishStart()
	preferred := 0
	if m.cfg.PreferredPort != nil {
		preferred = m.cfg.PreferredPort()
	}
	port, err := FreePort(preferred)
	if err != nil {
		return m.fail(Status{}, fmt.Errorf("空いているポートを選べません: %w", err)), err
	}
	spec, err := m.cfg.Build(port)
	if err != nil {
		return m.fail(Status{Port: port}, err), err
	}
	spec.Name = strings.ToLower(m.cfg.Name)
	url := "http://127.0.0.1:" + strconv.Itoa(port)
	m.set(Status{State: StateStarting, Port: port, URL: url})
	r, err := launch(spec, m.cfg.Logger, nil)
	if err != nil {
		return m.fail(Status{Port: port, URL: url}, fmt.Errorf("%s を起動できません: %w", m.cfg.Name, err)), err
	}
	started := time.Now()
	m.mu.Lock()
	m.cur = r
	m.mu.Unlock()
	go m.watchExit(r, port, url)

	readyCtx, cancel := context.WithTimeout(ctx, m.cfg.StartTimeout)
	defer cancel()
	if err := m.waitReady(readyCtx, r, url); err != nil {
		if r.stopping() { // Stop was called during the start
			m.set(Status{State: StateStopped})
			return m.Status(), context.Canceled
		}
		m.kill(r)
		reason := fmt.Errorf("%s が起動しませんでした: %w%s", m.cfg.Name, err, r.logTail())
		return m.fail(Status{Port: port, URL: url, ExitCode: r.exitCode()}, reason), reason
	}
	s := Status{State: StateRunning, Port: port, URL: url, PID: r.pid(), StartedAt: started}
	m.set(s)
	return s, nil
}

// fail records a failed start or exit.
func (m *Manager) fail(s Status, err error) Status {
	s.State, s.Error = StateFailed, err.Error()
	m.set(s)
	return s
}

func (m *Manager) waitReady(ctx context.Context, r *run, url string) error {
	tick := time.NewTicker(m.cfg.PollEvery)
	defer tick.Stop()
	for {
		if err := m.cfg.Ready(ctx, url); err == nil {
			return nil
		}
		select {
		case <-r.done:
			return fmt.Errorf("プロセスが終了しました（終了コード %d）", r.exitCode())
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%s 以内に応答しませんでした", m.cfg.StartTimeout)
			}
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// watchExit reports an exit nobody asked for while the child was running.
func (m *Manager) watchExit(r *run, port int, url string) {
	<-r.done
	m.mu.Lock()
	current := m.cur == r
	if current {
		m.cur = nil
	}
	wasRunning := m.status.State == StateRunning && m.status.PID == r.pid()
	m.mu.Unlock()
	if current && wasRunning && !r.stopping() {
		err := fmt.Errorf("%s が予期せず終了しました（終了コード %d）%s", m.cfg.Name, r.exitCode(), r.logTail())
		m.cfg.Logger.Warn("child exited", "name", m.cfg.Name, "exit_code", r.exitCode())
		m.fail(Status{Port: port, URL: url, ExitCode: r.exitCode()}, err)
	}
}

// Stop ends the child and everything it started, and waits for it. Stopping a stopped (or failed) child is a no-op.
func (m *Manager) Stop() error {
	m.mu.Lock()
	r := m.cur
	m.mu.Unlock()
	if r == nil {
		if m.Status().State != StateFailed {
			m.set(Status{State: StateStopped})
		}
		return nil
	}
	m.kill(r)
	m.set(Status{State: StateStopped})
	return nil
}

// kill ends r politely, then for good.
func (m *Manager) kill(r *run) {
	r.markStopping()
	r.terminate(m.cfg.StopTimeout)
	m.mu.Lock()
	if m.cur == r {
		m.cur = nil
	}
	m.mu.Unlock()
}

// FreePort is the preferred port when it is free on the loopback interface, else a free port the system picks.
func FreePort(preferred int) (int, error) {
	for _, p := range []int{preferred, 0} {
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err != nil {
			continue
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		return port, nil
	}
	return 0, errors.New("no free loopback port")
}
