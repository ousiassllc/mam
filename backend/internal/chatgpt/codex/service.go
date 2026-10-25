// Package codex is the "Codex 経由" ChatGPT login of mam (spec/architecture/chatgpt-signin.md §8): the Codex app-server,
// downloaded at a pinned version or set by path, started as a child process, used to sign in with a ChatGPT account of
// any plan (including a Business / Enterprise workspace) and to generate text with that plan.
//
// mam never reads, keeps or returns a token: the app-server stores and refreshes them itself (in the OS keyring, in a
// CODEX_HOME of mam's own). What mam holds is facts: the e-mail address, the plan, the usage limits. Text generation
// only; images are never generated through this route.
package codex

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// InstructionMode says where the system prompt of an Agent goes.
type InstructionMode string

const (
	// InstructionsBase puts it in baseInstructions: it replaces Codex's default instructions (written for a coding
	// agent), which a mam Agent does not want. This is the first candidate of the Issue (#845 decision 8).
	InstructionsBase InstructionMode = "base"
	// InstructionsDeveloper puts it in developerInstructions: it is added to Codex's default instructions.
	InstructionsDeveloper InstructionMode = "developer"
)

// DefaultInstructions is the switch point of decision 8: change this one value to move every system prompt from
// baseInstructions to developerInstructions if a comparison on a real machine shows a problem with the first. The comparison is not done yet.
const DefaultInstructions = InstructionsBase

// Directory names below the data directory.
const (
	codexHomeDir = "codex-home"
	workDirName  = "codex-work"
)

// Options build a Service.
type Options struct {
	// DataDir is mam's data directory: the CODEX_HOME, the working directory of the turns and StateFile live in it.
	DataDir string
	// Layout is where the downloaded engine lies (the directory next to mam.exe). An empty Root disables the download.
	Layout engine.Layout
	// Version is the mam version (clientInfo.version).
	Version string
	// Platform and Arch default to runtime.GOOS and runtime.GOARCH. Only "windows" has the download.
	Platform, Arch string
	// Catalog is the pinned Codex; the zero value selects the embedded one.
	Catalog catalog.Catalog
	// Keys is the OS secure storage, used only to find out whether it works (the app-server uses the keyring itself).
	Keys credential.Store
	// Open opens a URL in the default browser of this machine; nil opens none.
	Open func(url string) error
	// Instructions selects where the system prompt goes; "" selects DefaultInstructions.
	Instructions InstructionMode
	Logger       *slog.Logger
	// The following are for tests.
	Fetch        fetch.Options
	FreeBytes    func(string) (uint64, error)
	Start        func(appserver.Launch) (appserver.Process, error)
	Now          func() time.Time
	LoginTimeout time.Duration
}

// Service is the Codex login of mam. It is safe for concurrent use.
type Service struct {
	opts      Options
	cat       catalog.Catalog
	supported bool // the engine can be downloaded here
	installer *engine.Installer
	srv       *appserver.Server
	state     *stateStore
	home      string // CODEX_HOME
	work      string // working directory of the turns

	loginMu sync.Mutex
	login   *activeLogin

	mu          sync.Mutex
	rateWindows []Window
	models      modelCache
	install     installState
	credsStore  string // keyring or auto, decided on the first start

	closed bool
}

// New builds the Service. Nothing is started and nothing is downloaded.
func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Platform == "" {
		o.Platform = runtime.GOOS
	}
	if o.Arch == "" {
		o.Arch = runtime.GOARCH
	}
	if o.Instructions == "" {
		o.Instructions = DefaultInstructions
	}
	cat := o.Catalog
	if cat.Version == "" {
		cat = catalog.Default()
	}
	s := &Service{opts: o, cat: cat, home: filepath.Join(o.DataDir, codexHomeDir), work: filepath.Join(o.DataDir, workDirName),
		state: openState(filepath.Join(o.DataDir, StateFile))}
	if asset, ok := cat.AssetFor(o.Arch); ok && o.Platform == "windows" && o.Layout.Root != "" {
		s.supported = true
		s.installer = &engine.Installer{Layout: o.Layout, Version: cat.Version, Asset: asset, Fetch: o.Fetch, FreeBytes: o.FreeBytes, Now: o.Now}
		s.installer.CleanLeftovers()
	}
	s.srv = appserver.New(appserver.Options{Start: s.startProcess, CodexHome: s.home, Version: o.Version, Logger: o.Logger})
	s.srv.SetAccountListener(s.onNotification)
	return s
}

func (s *Service) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

// exePath is the app-server to run: the path the user set, else the installed engine of the pinned version.
func (s *Service) exePath() (path string, custom bool, err error) {
	if p := s.state.get().ExePath; p != "" {
		if st, serr := os.Stat(p); serr != nil || !st.Mode().IsRegular() {
			return "", true, ErrEngineNotInstalled
		}
		return p, true, nil
	}
	if s.installer != nil {
		if p, ok := s.installer.ExePath(); ok {
			return p, false, nil
		}
	}
	return "", false, ErrEngineNotInstalled
}

// startProcess is the start function of the app-server: it prepares mam's own directories and launches the engine.
func (s *Service) startProcess() (appserver.Process, error) {
	exe, _, err := s.exePath()
	if err != nil {
		return nil, err
	}
	for _, d := range []string{s.home, s.work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	l := appserver.Launch{Exe: exe, CodexHome: s.home, WorkDir: s.work, CredentialsStore: s.credentialsStore(), Logger: s.opts.Logger}
	if s.opts.Start != nil {
		return s.opts.Start(l)
	}
	return l.Start()
}

// credentialsStore is the value of cli_auth_credentials_store: the OS keyring when it works (a probe through mam's own
// credential store), else "auto", which the app-server falls back from to a file in CODEX_HOME.
func (s *Service) credentialsStore() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.credsStore == "" {
		s.credsStore = protocol.CredentialsAuto
		if s.opts.Keys != nil && s.opts.Keys.Set(probeKey, "probe") == nil {
			_ = s.opts.Keys.Delete(probeKey)
			s.credsStore = protocol.CredentialsKeyring
		}
	}
	return s.credsStore
}

const probeKey = "chatgpt-codex-probe"

// session is the running app-server, started when needed; ErrEngineNotInstalled when there is nothing to start.
func (s *Service) session(ctx context.Context) (*appserver.Session, error) {
	return s.srv.Ensure(ctx)
}

// Close stops the app-server for good (mam shuts down) and cancels an install in progress.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	cancel := s.install.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.srv.Close()
}
