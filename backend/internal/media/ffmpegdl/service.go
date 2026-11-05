package ffmpegdl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// ErrUnsupported means ffmpeg is not downloaded for this platform (only Windows x64 and macOS on Apple silicon). The user installs
// it themselves there.
var ErrUnsupported = errors.New("ffmpegdl: no ffmpeg download for this platform")

// ErrBusy means an install or a removal is running: a removal refuses to start under an install, and an install refuses to
// start while the downloaded copy is being removed.
var ErrBusy = errors.New("ffmpegdl: an install or a removal is running")

// Options of New.
type Options struct {
	Layout  Layout
	Catalog Catalog
	// GOOS and GOARCH are the platform the catalog is read for.
	GOOS, GOARCH string
	// Fetch are the options of the downloads (tests give the client and URL check of a test server).
	Fetch     fetch.Options
	FreeBytes func(path string) (uint64, error)
	// ClearQuarantine and Check replace the ones of Installer (tests).
	ClearQuarantine func(path string) error
	Check           func(ctx context.Context, exe string) error
	Logger          *slog.Logger
}

// Service is the download of ffmpeg in the background: the one install that may run at a time, its progress, and the state of
// what is installed. Nothing starts but by StartInstall (a click of the user): there is no update check and no background
// network call.
type Service struct {
	installer *Installer
	supported bool
	logger    *slog.Logger

	// install is the download in the background (exeinstall: shared with Codex); Remove holds it while the files are deleted.
	install *exeinstall.Runner

	// removeFiles deletes the downloaded copy (the installer's Remove; tests replace it to hold a removal in the middle).
	removeFiles func() error
}

// New is the service. A platform with no pinned build is a service that is not Supported: StartInstall refuses, and Dirs is empty.
func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	o.Fetch.OnRetry = exeinstall.RetryLogger(o.Logger, "ffmpeg", o.Fetch.OnRetry) // an automatic resume of a download is logged
	p, ok := o.Catalog.For(o.GOOS, o.GOARCH)
	layout := o.Layout
	if !ok || layout.Root == "" {
		ok = false
	}
	s := &Service{
		supported: ok,
		logger:    o.Logger,
		installer: &Installer{
			Layout: layout, Platform: p, Fetch: o.Fetch, FreeBytes: o.FreeBytes, ClearQuarantine: o.ClearQuarantine, Check: o.Check,
		},
	}
	s.removeFiles = s.installer.Remove
	s.install = s.newInstall()
	if ok {
		// What an interrupted install left (a half-extracted bin.new, or a swap that stopped between its two renames) is
		// cleaned, or put back, at startup (spec/architecture/ffmpeg-download.md §4.6).
		s.installer.CleanLeftovers()
	}
	return s
}

// Status is what the screen shows: what can be downloaded and what is installed.
type Status struct {
	// Supported: ffmpeg can be downloaded here.
	Supported bool
	// Pinned: the build the catalog names (zero when not Supported).
	Pinned         Platform
	DownloadBytes  int64
	ExtractedBytes int64
	// Dir is where the installed executables lie (empty when not Supported).
	Dir             string
	Installed       bool
	Version         string
	UpdateAvailable bool
	Broken          bool
	Install         exeinstall.Status
}

// Status reads what is installed and the progress of the install in hand.
func (s *Service) Status() Status {
	if !s.supported {
		return Status{Install: exeinstall.Status{State: exeinstall.Idle}}
	}
	st := s.installer.Status()
	p := s.installer.Platform
	return Status{
		Supported: true, Pinned: p, DownloadBytes: p.DownloadBytes(), ExtractedBytes: p.ExtractedBytes(), Dir: s.installer.Layout.BinDir(),
		Installed: st.Installed, Version: st.Version, UpdateAvailable: st.UpdateAvailable, Broken: st.Broken, Install: s.install.Status(),
	}
}

// Dirs is the directory the search of media/locate gets after PATH and the well-known places (locate.Finder.Extra): the one the
// complete install is in, or nothing. It is read from the disk on every call, so an install that finishes is found by the next
// lookup.
func (s *Service) Dirs() []string {
	if !s.supported {
		return nil
	}
	return s.installer.Dirs()
}

// StartInstall downloads the pinned ffmpeg in the background (progress: Status) and installs it. It refuses at once where the
// download is not offered (ErrUnsupported), the directory cannot be written to (exeinstall.ErrNotWritable) or the disk lacks the
// room (*fetch.NoSpaceError). started is false when an install is already running (ErrBusy: the downloaded copy is being removed). Installing again resumes an interrupted
// download and, with a newer pinned version, is the update.
func (s *Service) StartInstall() (started bool, err error) {
	if !s.supported {
		return false, ErrUnsupported
	}
	return s.install.Start()
}

// newInstall is the install of ffmpeg: the start, the stop and the progress of it are the Runner's (exeinstall), shared with the
// download of Codex; what is ffmpeg's own is the checks before it, the progress it reports and the words of its ending.
func (s *Service) newInstall() *exeinstall.Runner {
	return exeinstall.NewRunner(exeinstall.RunnerConfig{
		Kind:        worktrack.KindFFmpegInstall,
		Subject:     "ffmpeg",
		Starting:    exeinstall.Status{Phase: string(PhaseDownload), Message: phaseMessage[PhaseDownload]},
		ErrClosed:   errors.New("ffmpegdl: mam is shutting down"),
		ErrRemoving: ErrBusy,
		Preflight: func() error {
			if err := s.installer.Layout.CheckWritable(); err != nil {
				return err
			}
			return s.installer.CheckSpace()
		},
		Install: func(ctx context.Context, report func(exeinstall.Status)) error {
			return s.installer.Install(ctx, func(p Phase, done, total int64) {
				share := phaseShare[p]
				pct := share[0]
				if total > 0 {
					pct += int(float64(done) / float64(total) * float64(share[1]-share[0]))
				}
				report(exeinstall.Status{Phase: string(p), Percent: pct, Done: done, Total: total, Message: phaseMessage[p]})
			})
		},
		Succeeded: "ffmpeg を入れました",
		Failed: func(err error) string {
			s.logger.Warn("ffmpeg download failed", "err", err)
			return Explain(err)
		},
	})
}

// phaseShare is how much of the bar each phase fills.
var phaseShare = map[Phase][2]int{PhaseDownload: {0, 85}, PhaseExtract: {85, 98}, PhaseCheck: {98, 99}}

var phaseMessage = map[Phase]string{
	PhaseDownload: "ffmpeg をダウンロードしています",
	PhaseExtract:  "ffmpeg を展開して確認しています",
	PhaseCheck:    "ffmpeg が動くか確かめています",
}

// Works is the installs of this process, for the job list (worktrack.Source): the one that runs and the latest that ended. It is
// memory only: a restart forgets them.
func (s *Service) Works() []worktrack.Record { return s.install.Works() }

// Explain is why an install failed, in words for the user.
func Explain(err error) string {
	var herr *fetch.HTTPError
	switch {
	case fetch.IsConnectionLost(err):
		return fetch.ConnectionLostMessage
	case errors.Is(err, fetch.ErrChecksum), errors.Is(err, fetch.ErrSize):
		return "ダウンロードしたファイルが、mam が確かめた版と一致しませんでした（SHA-256 またはサイズの不一致）。ファイルは使わずに削除しました。配布元が差し替わったか、通信の途中で書き換わった可能性があります"
	case errors.Is(err, exeinstall.ErrMismatch), errors.Is(err, exeinstall.ErrUnsafeArchive):
		return "ダウンロードしたアーカイブの中身が、mam が確かめた版と一致しませんでした。ファイルは使わずに削除しました"
	case errors.Is(err, ErrNotRunnable):
		return "ダウンロードした ffmpeg を実行できませんでした。セキュリティ製品に止められていないか確認してください。何も入れていません: " + err.Error()
	case errors.As(err, &herr):
		if herr.Status == http.StatusNotFound || herr.Status == http.StatusGone {
			return fmt.Sprintf("配布元に、この mam が使う版のファイルがありません（HTTP %d）。mam を新しい版に更新してください", herr.Status)
		}
		return fmt.Sprintf("配布元がエラーを返しました（HTTP %d %s）。しばらくしてからもう一度お試しください", herr.Status, http.StatusText(herr.Status))
	}
	return "ffmpeg を入れられませんでした: " + err.Error()
}

// CancelInstall stops an install in progress; the part file stays, so that installing again resumes.
func (s *Service) CancelInstall() { s.install.Cancel() }

// WaitInstall waits for the install in progress to end (tests and shutdown).
func (s *Service) WaitInstall() { s.install.Wait() }

// Remove deletes the downloaded ffmpeg. It refuses (ErrBusy) while an install runs or another removal is in progress, and while
// it deletes, StartInstall refuses (ErrBusy): the files are never deleted under a running install.
func (s *Service) Remove() error {
	if !s.supported {
		return ErrUnsupported
	}
	release, ok := s.install.Hold(true)
	if !ok {
		return ErrBusy
	}
	defer release()
	s.install.Reset()
	return s.removeFiles()
}

// Close stops an install in progress and refuses new ones (shutdown).
func (s *Service) Close() { s.install.Close() }
