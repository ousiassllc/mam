package ffmpegdl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// ErrUnsupported means ffmpeg is not downloaded for this platform (only Windows x64 and macOS on Apple silicon). The user installs
// it themselves there.
var ErrUnsupported = errors.New("ffmpegdl: no ffmpeg download for this platform")

// ErrBusy means an install or a removal is running.
var ErrBusy = errors.New("ffmpegdl: an install is running")

// InstallState is where the download stands.
type InstallState string

// The states of an install.
const (
	InstallIdle      InstallState = "idle"
	InstallRunning   InstallState = "running"
	InstallSucceeded InstallState = "succeeded"
	InstallFailed    InstallState = "failed"
)

// InstallStatus is the progress of the download.
type InstallStatus struct {
	State   InstallState
	Phase   Phase
	Percent int
	Done    int64
	Total   int64
	Message string
	// Error says why the install failed, for the user. Cancelled: the user stopped it (not a failure; Error is then empty).
	Error     string
	Cancelled bool
}

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

	mu      sync.Mutex
	install InstallStatus
	cancel  context.CancelFunc
	closed  bool
	wg      sync.WaitGroup
}

// New is the service. A platform with no pinned build is a service that is not Supported: StartInstall refuses, and Dirs is empty.
func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	p, ok := o.Catalog.For(o.GOOS, o.GOARCH)
	layout := o.Layout
	if !ok || layout.Root == "" {
		ok = false
	}
	return &Service{
		supported: ok,
		logger:    o.Logger,
		installer: &Installer{
			Layout: layout, Platform: p, Fetch: o.Fetch, FreeBytes: o.FreeBytes, ClearQuarantine: o.ClearQuarantine, Check: o.Check,
		},
	}
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
	Install         InstallStatus
}

// Status reads what is installed and the progress of the install in hand.
func (s *Service) Status() Status {
	if !s.supported {
		return Status{Install: InstallStatus{State: InstallIdle}}
	}
	st := s.installer.Status()
	p := s.installer.Platform
	s.mu.Lock()
	in := s.install
	s.mu.Unlock()
	if in.State == "" {
		in.State = InstallIdle
	}
	return Status{
		Supported: true, Pinned: p, DownloadBytes: p.DownloadBytes(), ExtractedBytes: p.ExtractedBytes(), Dir: s.installer.Layout.BinDir(),
		Installed: st.Installed, Version: st.Version, UpdateAvailable: st.UpdateAvailable, Broken: st.Broken, Install: in,
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
// room (*fetch.NoSpaceError). started is false when an install is already running. Installing again resumes an interrupted
// download and, with a newer pinned version, is the update.
func (s *Service) StartInstall() (started bool, err error) {
	if !s.supported {
		return false, ErrUnsupported
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return false, errors.New("ffmpegdl: mam is shutting down")
	}
	if s.install.State == InstallRunning {
		s.mu.Unlock()
		cancel()
		return false, nil
	}
	prev := s.install
	s.install = InstallStatus{State: InstallRunning, Phase: PhaseDownload, Message: "ffmpeg をダウンロードしています"}
	s.cancel = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	abort := func(err error) (bool, error) {
		s.mu.Lock()
		s.install, s.cancel = prev, nil
		s.mu.Unlock()
		s.wg.Done()
		cancel()
		return false, err
	}
	if err := s.installer.Layout.CheckWritable(); err != nil {
		return abort(err)
	}
	if err := s.installer.CheckSpace(); err != nil {
		return abort(err)
	}
	go s.run(ctx)
	return true, nil
}

// phaseShare is how much of the bar each phase fills.
var phaseShare = map[Phase][2]int{PhaseDownload: {0, 85}, PhaseExtract: {85, 98}, PhaseCheck: {98, 99}}

var phaseMessage = map[Phase]string{
	PhaseDownload: "ffmpeg をダウンロードしています",
	PhaseExtract:  "ffmpeg を展開して確認しています",
	PhaseCheck:    "ffmpeg が動くか確かめています",
}

func (s *Service) run(ctx context.Context) {
	defer s.wg.Done()
	err := s.installer.Install(ctx, func(p Phase, done, total int64) {
		share := phaseShare[p]
		pct := share[0]
		if total > 0 {
			pct += int(float64(done) / float64(total) * float64(share[1]-share[0]))
		}
		s.mu.Lock()
		s.install = InstallStatus{State: InstallRunning, Phase: p, Percent: pct, Done: done, Total: total, Message: phaseMessage[p]}
		s.mu.Unlock()
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = nil
	switch {
	case err == nil:
		s.install = InstallStatus{State: InstallSucceeded, Percent: 100, Message: "ffmpeg を入れました"}
	case errors.Is(err, context.Canceled):
		s.install = InstallStatus{State: InstallFailed, Cancelled: true}
	default:
		s.logger.Warn("ffmpeg download failed", "err", err)
		s.install = InstallStatus{State: InstallFailed, Error: Explain(err)}
	}
}

// Explain is why an install failed, in words for the user.
func Explain(err error) string {
	var herr *fetch.HTTPError
	switch {
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
func (s *Service) CancelInstall() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WaitInstall waits for the install in progress to end (tests and shutdown).
func (s *Service) WaitInstall() { s.wg.Wait() }

// Remove deletes the downloaded ffmpeg. It refuses (ErrBusy) while an install runs.
func (s *Service) Remove() error {
	if !s.supported {
		return ErrUnsupported
	}
	s.mu.Lock()
	if s.install.State == InstallRunning {
		s.mu.Unlock()
		return ErrBusy
	}
	s.install = InstallStatus{State: InstallIdle}
	s.mu.Unlock()
	return s.installer.Remove()
}

// Close stops an install in progress and refuses new ones (shutdown).
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}
