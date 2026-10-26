package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/mcp/bind"
	"github.com/ousiassllc/mam/backend/internal/service/mcpsettings"
)

// DefaultPort is the preferred port of the MCP listener; an OS-chosen port is used when it is taken.
const DefaultPort = 8788

// shutdownTimeout is how long an old listener's requests get to finish after a rebind or at exit.
// A server-sent-events stream never finishes by itself, so this is also how long a rebind keeps it.
var shutdownTimeout = 5 * time.Second

// ManagerOptions configure a Manager.
type ManagerOptions struct {
	Handler http.Handler
	// SettingsPath is mcp.json of the data directory.
	SettingsPath string
	// PreferredPort is tried first on every bind (DefaultPort); 0 asks the OS every time.
	PreferredPort int
	Logger        *slog.Logger
	// LocalAddrs lists the addresses of the machine's interfaces; nil selects bind.LocalAddrs.
	LocalAddrs func() ([]netip.Addr, error)
	// Listen opens a TCP listener; nil selects net.Listen. A seam for tests.
	Listen func(network, address string) (net.Listener, error)
}

type running struct {
	host string
	ln   net.Listener
	srv  *http.Server
	done chan struct{}
}

// Manager owns the listener of the MCP server. Start opens it on the saved bind address; Rebind moves it
// to another address while the app runs: the new listener is opened first, so a failure leaves the old
// one serving. All methods are safe for concurrent use.
type Manager struct {
	opts ManagerOptions

	mu      sync.Mutex
	cur     *running
	saved   string
	problem string
	closed  bool
}

// NewManager returns a Manager that is not listening yet.
func NewManager(opts ManagerOptions) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.LocalAddrs == nil {
		opts.LocalAddrs = bind.LocalAddrs
	}
	if opts.Listen == nil {
		opts.Listen = net.Listen
	}
	return &Manager{opts: opts, saved: bind.Default}
}

// Start opens the listener on the saved bind address. When that address cannot be used now (the file is
// unreadable, the tailnet interface is not up yet, the address is not allowed any more) it listens on
// the loopback address instead and keeps the reason in Status.Problem: MCP stays reachable on the
// machine and never on a wider address than the person chose. It returns an error only when not even the
// loopback address can be opened.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	saved, err := bind.Load(m.opts.SettingsPath)
	if err != nil {
		m.problem = "mcp.json を読み込めないため、127.0.0.1 で待ち受けています: " + err.Error()
		saved = bind.Default
	}
	m.saved = saved
	host := saved
	if _, err := m.validate(saved); err != nil {
		m.problem = fmt.Sprintf("保存済みの待ち受け先 %s を使えないため、127.0.0.1 で待ち受けています: %v", saved, err)
		host = bind.Default
	}
	r, err := m.open(host)
	if err != nil && host != bind.Default {
		m.problem += fmt.Sprintf("（%v）", err)
		r, err = m.open(bind.Default)
	}
	if err != nil {
		m.problem = "MCP サーバーを起動できません: " + err.Error()
		return err
	}
	m.cur = r
	m.opts.Logger.Info("mcp server started", "addr", r.ln.Addr().String(), "problem", m.problem)
	if m.problem != "" {
		m.opts.Logger.Warn("mcp server is not on the saved address", "saved", saved, "problem", m.problem)
	}
	return nil
}

func (m *Manager) validate(host string) (netip.Addr, error) {
	local, err := m.opts.LocalAddrs()
	if err != nil {
		return netip.Addr{}, err
	}
	return bind.Validate(host, local)
}

// open listens on host at the preferred port, or at a port the OS chooses when that is taken, and serves.
func (m *Manager) open(host string) (*running, error) {
	var ln net.Listener
	if m.opts.PreferredPort > 0 {
		ln, _ = m.opts.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(m.opts.PreferredPort))) // busy: fall back below
	}
	if ln == nil {
		var err error
		if ln, err = m.opts.Listen("tcp", net.JoinHostPort(host, "0")); err != nil {
			return nil, fmt.Errorf("listen on %s: %w", host, err)
		}
	}
	srv := &http.Server{
		Handler:           m.opts.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No read or write timeout: tool calls take a while, uploads are large and the event stream is long-lived.
	}
	r := &running{host: host, ln: ln, srv: srv, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.opts.Logger.Error("mcp server stopped", "err", err)
		}
	}()
	return r, nil
}

// Rebind moves the listener to host and saves it in mcp.json. host must pass bind.Validate. The new
// listener is opened and the setting saved before the old listener is closed, so any failure leaves the
// old one serving and the setting unchanged. Requests in flight on the old listener get a few seconds to finish.
func (m *Manager) Rebind(host string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mcp server is stopped")
	}
	ip, err := m.validate(host)
	if err != nil {
		return err
	}
	host = ip.String()
	if m.cur != nil && m.cur.host == host {
		return m.save(host) // already there: only the saved choice may differ
	}
	next, err := m.open(host)
	if err != nil {
		return err
	}
	if err := m.save(host); err != nil {
		stop(next, 0)
		return err
	}
	old := m.cur
	m.cur, m.problem = next, ""
	m.opts.Logger.Info("mcp server moved", "addr", next.ln.Addr().String())
	if old != nil {
		go stop(old, shutdownTimeout)
	}
	return nil
}

func (m *Manager) save(host string) error {
	if err := bind.Save(m.opts.SettingsPath, host); err != nil {
		return fmt.Errorf("save %s: %w", bind.FileName, err)
	}
	m.saved = host
	return nil
}

// Status is the state of the listener for the settings screen.
func (m *Manager) Status() mcpsettings.Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := mcpsettings.Status{Saved: m.saved, Problem: m.problem}
	if m.cur != nil {
		s.Addr = m.cur.ln.Addr().String()
	}
	if local, err := m.opts.LocalAddrs(); err == nil {
		s.Candidates = bind.Candidates(local)
	} else {
		s.Candidates = []string{bind.Default}
	}
	return s
}

// Addr is the address listened on now, "" when not listening.
func (m *Manager) Addr() string { return m.Status().Addr }

// Close stops the listener: in-flight requests get a few seconds, the rest are cut. It is safe to call twice.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	cur := m.cur
	m.cur = nil
	m.mu.Unlock()
	if cur != nil {
		stop(cur, shutdownTimeout)
	}
}

// stop shuts r down gracefully for at most grace (0: at once) and waits for its Serve to return.
func stop(r *running, grace time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := r.srv.Shutdown(ctx); err != nil {
		_ = r.srv.Close()
	}
	<-r.done
}
