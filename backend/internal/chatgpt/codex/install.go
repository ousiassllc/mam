package codex

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// newInstall is the install of the engine: the start, the stop and the progress of it are the Runner's (exeinstall), shared with
// the download of ffmpeg; what is Codex's own is the checks before it, the progress it reports and the words of its ending.
func (s *Service) newInstall() *exeinstall.Runner {
	return exeinstall.NewRunner(exeinstall.RunnerConfig{
		Kind:        worktrack.KindCodexInstall,
		Subject:     "Codex",
		Starting:    exeinstall.Status{Phase: string(engine.PhaseDownload), Message: "Codex をダウンロードしています"},
		ErrClosed:   errors.New("codex: mam is shutting down"),
		ErrRemoving: ErrEngineRemoving,
		Preflight: func() error {
			if err := s.installer.Layout.CheckWritable(); err != nil {
				return err
			}
			if err := s.installer.CheckSpace(); err != nil {
				return err
			}
			s.srv.Stop() // the files are replaced: the engine must not be running
			return nil
		},
		Install:   s.runInstall,
		Succeeded: "導入しました",
		Cancelled: exeinstall.Status{Error: "中断しました。もう一度導入すると、ダウンロード済みの分から再開します"},
		Failed: func(err error) string {
			s.opts.Logger.Warn("codex engine install failed", "err", sanitize(err.Error()))
			return installFailure(err)
		},
	})
}

func (s *Service) runInstall(ctx context.Context, report func(exeinstall.Status)) error {
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
		report(exeinstall.Status{Phase: string(p), Percent: pct, Done: done, Total: total, Message: msg})
	})
	// Starts are refused while the state is running: this clears the failed status those refusals left.
	s.srv.Stop()
	return err
}

// installFailure is why an install failed, in words for the user. A download that broke on every transfer that was allowed is
// told as such (the part file stays and installing again continues from it), not as the English error of the connection.
func installFailure(err error) string {
	if fetch.IsConnectionLost(err) {
		return fetch.ConnectionLostMessage
	}
	return fmt.Sprintf("Codex を導入できませんでした: %s", sanitize(err.Error()))
}

// InstallStatus is the progress of the engine download (State idle when none was started).
func (s *Service) InstallStatus() exeinstall.Status { return s.install.Status() }

// StartInstall downloads the pinned engine in the background (progress: InstallStatus) and installs it. It refuses at
// once where the download is not available (ErrUnsupported), the engine directory cannot be written to
// (exeinstall.ErrNotWritable) or the disk lacks the room (*fetch.NoSpaceError). started is false when an install is already
// running (ErrEngineRemoving: the files are being deleted by a logout). Installing again resumes an interrupted download
// and, with a newer pinned version, is the update.
func (s *Service) StartInstall() (started bool, err error) {
	if !s.supported {
		return false, ErrUnsupported
	}
	return s.install.Start()
}

// Works is the installs of this process, for the job list (worktrack.Source): the one that runs and the latest that ended. It is
// memory only: a restart forgets them.
func (s *Service) Works() []worktrack.Record { return s.install.Works() }

// CancelInstall stops an install in progress; the part file stays, so that installing again resumes.
func (s *Service) CancelInstall() { s.install.Cancel() }

// WaitInstall waits for the install in progress to end (tests and shutdown).
func (s *Service) WaitInstall() { s.install.Wait() }
