package codex

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
)

// InstallState is where the engine download stands.
type InstallState string

// The states of an install.
const (
	InstallIdle      InstallState = "idle"
	InstallRunning   InstallState = "running"
	InstallSucceeded InstallState = "succeeded"
	InstallFailed    InstallState = "failed"
)

// InstallStatus is the progress of the engine download.
type InstallStatus struct {
	State   InstallState
	Phase   string // download or extract
	Percent int
	Done    int64
	Total   int64
	Message string
	// Error says why the install failed (for the user).
	Error string
}

type installState struct {
	InstallStatus
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// InstallStatus is the progress of the engine download (State idle when none was started).
func (s *Service) InstallStatus() InstallStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.install.State == "" {
		return InstallStatus{State: InstallIdle}
	}
	return s.install.InstallStatus
}

// StartInstall downloads the pinned engine in the background (progress: InstallStatus) and installs it. It refuses at
// once where the download is not available (ErrUnsupported), the engine directory cannot be written to
// (engine.ErrNotWritable) or the disk lacks the room (*fetch.NoSpaceError). started is false when an install is already
// running. Installing again resumes an interrupted download and, with a newer pinned version, is the update.
func (s *Service) StartInstall() (started bool, err error) {
	if !s.supported {
		return false, ErrUnsupported
	}
	// The state is set under the lock of the check, so that two callers cannot both start, and starts of the engine are
	// refused from then on (startProcess): the checks below and the stop of the engine take a while.
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return false, errors.New("codex: mam is shutting down")
	}
	if s.install.State == InstallRunning {
		s.mu.Unlock()
		cancel()
		return false, nil
	}
	prev := s.install.InstallStatus
	s.install.InstallStatus = InstallStatus{State: InstallRunning, Phase: string(engine.PhaseDownload), Message: "Codex をダウンロードしています"}
	s.install.cancel = cancel
	s.install.wg.Add(1)
	s.mu.Unlock()
	abort := func(err error) (bool, error) {
		s.mu.Lock()
		s.install.InstallStatus, s.install.cancel = prev, nil
		s.mu.Unlock()
		s.install.wg.Done()
		cancel()
		return false, err
	}
	if err := s.installer.Layout.CheckWritable(); err != nil {
		return abort(err)
	}
	if err := s.installer.CheckSpace(); err != nil {
		return abort(err)
	}
	s.srv.Stop() // the files are replaced: the engine must not be running
	go s.runInstall(ctx)
	return true, nil
}

func (s *Service) runInstall(ctx context.Context) {
	defer s.install.wg.Done()
	err := s.installer.Install(ctx, func(p engine.Phase, done, total int64) {
		pct := 0
		if total > 0 {
			pct = int(float64(done) / float64(total) * 70)
			if p == engine.PhaseExtract {
				pct = 70 + int(float64(done)/float64(total)*30)
			}
		}
		msg := "Codex をダウンロードしています"
		if p == engine.PhaseExtract {
			msg = "Codex を展開して確認しています"
		}
		s.mu.Lock()
		s.install.InstallStatus = InstallStatus{State: InstallRunning, Phase: string(p), Percent: pct, Done: done, Total: total, Message: msg}
		s.mu.Unlock()
	})
	// Starts are refused while the state is running: this clears the failed status those refusals left.
	s.srv.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.install.cancel = nil
	switch {
	case err == nil:
		s.install.InstallStatus = InstallStatus{State: InstallSucceeded, Percent: 100, Message: "導入しました"}
	case errors.Is(err, context.Canceled):
		s.install.InstallStatus = InstallStatus{State: InstallFailed, Error: "中断しました。もう一度導入すると、ダウンロード済みの分から再開します"}
	default:
		s.opts.Logger.Warn("codex engine install failed", "err", sanitize(err.Error()))
		s.install.InstallStatus = InstallStatus{State: InstallFailed, Error: fmt.Sprintf("Codex を導入できませんでした: %s", sanitize(err.Error()))}
	}
}

// CancelInstall stops an install in progress; the part file stays, so that installing again resumes.
func (s *Service) CancelInstall() {
	s.mu.Lock()
	cancel := s.install.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WaitInstall waits for the install in progress to end (tests and shutdown).
func (s *Service) WaitInstall() { s.install.wg.Wait() }
