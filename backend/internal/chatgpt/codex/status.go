package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/catalog"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// EngineState is where the engine stands.
type EngineState string

// The engine states of the settings screen and get_chatgpt_status.
const (
	EngineNotInstalled EngineState = "not_installed"
	EngineInstalling   EngineState = "installing"
	EngineReady        EngineState = "ready"
	EngineRunning      EngineState = "running"
	EngineFailed       EngineState = "failed"
)

// EngineSource is where the engine comes from.
type EngineSource string

// The engine sources.
const (
	SourceNone     EngineSource = ""
	SourceDownload EngineSource = "download"
	SourceCustom   EngineSource = "custom"
)

// Window is one usage window of the plan's Codex usage limit.
type Window struct {
	Name          string
	UsedPercent   int
	WindowMinutes int64
	ResetsAt      time.Time // zero when unknown
}

// Status is the state of the Codex login as the screens and the MCP tools show it. It holds no token. Reading it
// starts nothing: the account is the one last read (AccountView.CheckedAt); Refresh reads it again.
type Status struct {
	// Supported is whether the engine can be downloaded here (Windows only); elsewhere set a path.
	Supported bool
	Engine    EngineState
	Source    EngineSource
	// Version is the Codex version of the engine: the installed one for a download, "" for a path.
	Version string
	// PinnedVersion is the version mam was checked against; UpdateAvailable: the installed one differs, so it is not
	// used until the engine is installed again.
	PinnedVersion   string
	UpdateAvailable bool
	// CustomPath is the path the user set.
	CustomPath string
	// EngineError says why the engine is Failed (a crash, a start that did not complete).
	EngineError string
	Install     InstallStatus
	// CredentialsStore is keyring or auto ("" before the first start).
	CredentialsStore string
	Account          AccountView
	// RateLimits are the Codex usage windows as last read.
	RateLimits []Window
	License    catalog.License
	SourceURL  string
	// Dir is where the downloaded engine lies; CodexHome is mam's own CODEX_HOME.
	Dir       string
	CodexHome string
}

// Status describes the Codex login without starting anything.
func (s *Service) Status() Status {
	st := Status{
		Supported: s.supported, PinnedVersion: s.cat.Version, CustomPath: s.state.get().ExePath, Install: s.InstallStatus(),
		Account: s.state.get().Account, License: s.cat.License, SourceURL: s.cat.SourceURL, CodexHome: s.home,
	}
	if s.installer != nil {
		st.Dir = s.installer.Layout.Root
	}
	s.mu.Lock()
	st.RateLimits = append([]Window(nil), s.rateWindows...)
	st.CredentialsStore = s.credsStore
	s.mu.Unlock()

	if _, custom, err := s.exePath(); err == nil && custom {
		st.Source = SourceCustom
	} else if s.installer != nil {
		is := s.installer.Status()
		st.Version, st.UpdateAvailable = is.Version, is.UpdateAvailable
		if is.Installed && !custom {
			st.Source = SourceDownload
		}
	}
	srv := s.srv.Status()
	switch {
	case st.Install.State == InstallRunning:
		st.Engine = EngineInstalling
	case srv.State == appserver.StateRunning:
		st.Engine = EngineRunning
	case srv.State == appserver.StateFailed:
		st.Engine, st.EngineError = EngineFailed, srv.Error
	case st.Source != SourceNone && !st.UpdateAvailable:
		st.Engine = EngineReady
	default:
		st.Engine = EngineNotInstalled
	}
	return st
}

// Refresh starts the app-server if needed and reads the account and the usage limits, which are kept for Status.
func (s *Service) Refresh(ctx context.Context) (Status, error) {
	se, err := s.session(ctx)
	if err != nil {
		return s.Status(), err
	}
	var acc protocol.GetAccountResult
	if err := se.Call(ctx, protocol.MethodAccountRead, protocol.GetAccountParams{RefreshToken: false}, &acc); err != nil {
		return s.Status(), fmt.Errorf("codex: account/read: %w", err)
	}
	view := AccountView{}
	if acc.Account != nil && acc.Account.Type == protocol.AccountTypeChatGPT {
		view = AccountView{SignedIn: true, Type: acc.Account.Type, Email: acc.Account.Email, PlanType: acc.Account.PlanType}
	}
	s.setAccount(view)
	if view.SignedIn {
		var rl protocol.RateLimitsResult
		if err := se.Call(ctx, protocol.MethodRateLimitsRead, struct{}{}, &rl); err == nil {
			s.setRateLimits(rl.Codex())
		} else {
			s.opts.Logger.Warn("codex: account/rateLimits/read failed", "err", sanitize(err.Error()))
		}
	} else {
		s.setRateLimits(protocol.RateLimitSnapshot{})
	}
	return s.Status(), nil
}

func (s *Service) setAccount(v AccountView) {
	if v.SignedIn || v.CheckedAt.IsZero() {
		v.CheckedAt = s.now().UTC()
	}
	if err := s.state.update(func(d *stateData) { d.Account = v }); err != nil {
		s.opts.Logger.Warn("codex: could not save the account facts", "err", err)
	}
	if !v.SignedIn {
		s.setRateLimits(protocol.RateLimitSnapshot{})
	}
}

func (s *Service) setRateLimits(snap protocol.RateLimitSnapshot) {
	var ws []Window
	for _, w := range []*protocol.RateLimitWindow{snap.Primary, snap.Secondary} {
		if w == nil {
			continue
		}
		win := Window{UsedPercent: w.UsedPercent}
		if w.WindowDurationMins != nil {
			win.WindowMinutes = *w.WindowDurationMins
			win.Name = windowName(win.WindowMinutes)
		}
		if w.ResetsAt != nil {
			win.ResetsAt = time.Unix(*w.ResetsAt, 0).UTC()
		}
		ws = append(ws, win)
	}
	s.mu.Lock()
	s.rateWindows = ws
	s.mu.Unlock()
}

// windowName is the usual name of a window of that length.
func windowName(mins int64) string {
	switch {
	case mins <= 0:
		return ""
	case mins%(60*24*7) == 0:
		return fmt.Sprintf("%d 週間", mins/(60*24*7))
	case mins%(60*24) == 0:
		return fmt.Sprintf("%d 日", mins/(60*24))
	case mins%60 == 0:
		return fmt.Sprintf("%d 時間", mins/60)
	}
	return fmt.Sprintf("%d 分", mins)
}

// SetCustomPath sets the app-server to use instead of the downloaded one (a codex the user installed: the codex command
// or the dedicated app-server), or clears it with "". The path must be absolute and name a file. The running process
// is stopped so that the next use starts the new one. mam does not run the file to check it: a version other than the
// pinned one may speak a protocol mam does not know, which the handshake or the first call then reports.
func (s *Service) SetCustomPath(path string) error {
	if path != "" {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("codex: the path must be absolute: %q", path)
		}
		st, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("codex: %w", err)
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("codex: %q is not a file", path)
		}
		path = filepath.Clean(path)
	}
	if err := s.state.update(func(d *stateData) { d.ExePath = path }); err != nil {
		return err
	}
	s.srv.Stop()
	return nil
}
