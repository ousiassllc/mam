package ffmpegdl_test

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// newService is a Service for linux/amd64 (the test platform) over the rig's server and pinned platform.
func newService(t *testing.T, r *rig) *ffmpegdl.Service {
	t.Helper()
	s := ffmpegdl.New(ffmpegdl.Options{
		Layout:  r.ins.Layout,
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): r.ins.Platform}},
		GOOS:    "linux", GOARCH: "amd64",
		Fetch: fetch.Options{Client: r.srv.Client(), CheckURL: func(*url.URL) error { return nil }},
		Check: r.ins.Check,
	})
	t.Cleanup(s.Close)
	return s
}

func TestServiceInstallsInTheBackgroundAndTheCopyIsOfferedToTheSearch(t *testing.T) {
	r := newRig(t, "9.9.9")
	s := newService(t, r)
	st := s.Status()
	assert.True(t, st.Supported)
	assert.False(t, st.Installed)
	assert.Equal(t, ffmpegdl.InstallIdle, st.Install.State)
	assert.Equal(t, "9.9.9", st.Pinned.Version)
	assert.Empty(t, s.Dirs(), "nothing is found before the download")

	started, err := s.StartInstall()
	require.NoError(t, err)
	assert.True(t, started)
	s.WaitInstall()

	st = s.Status()
	assert.Equal(t, ffmpegdl.InstallSucceeded, st.Install.State, st.Install.Error)
	assert.Equal(t, 100, st.Install.Percent)
	assert.True(t, st.Installed)
	assert.Equal(t, "9.9.9", st.Version)
	assert.Equal(t, []string{r.ins.Layout.BinDir()}, s.Dirs())
}

func TestServiceRunsOneInstallAtATimeAndRefusesToRemoveDuringIt(t *testing.T) {
	r := newRig(t, "9.9.9")
	release, reached := make(chan struct{}), make(chan struct{}, 1)
	r.onServe = func(_ http.ResponseWriter, req *http.Request) bool {
		select {
		case reached <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-req.Context().Done():
			return true
		}
		return false
	}
	s := newService(t, r)
	started, err := s.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	<-reached
	assert.Equal(t, ffmpegdl.InstallRunning, s.Status().Install.State)
	assert.NotEmpty(t, s.Status().Install.Message)

	started, err = s.StartInstall()
	require.NoError(t, err)
	assert.False(t, started, "a second click while one runs starts nothing")
	require.ErrorIs(t, s.Remove(), ffmpegdl.ErrBusy)

	close(release)
	s.WaitInstall()
	assert.Equal(t, ffmpegdl.InstallSucceeded, s.Status().Install.State)

	require.NoError(t, s.Remove())
	st := s.Status()
	assert.False(t, st.Installed)
	assert.Equal(t, ffmpegdl.InstallIdle, st.Install.State)
	assert.Empty(t, s.Dirs())
}

func TestServiceReportsAWrongChecksumAndUsesNothing(t *testing.T) {
	r := newRig(t, "9.9.9")
	r.ins.Platform.Archives[0].SHA256 = strings.Repeat("0", 64)
	s := newService(t, r)
	started, err := s.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	s.WaitInstall()
	st := s.Status()
	assert.Equal(t, ffmpegdl.InstallFailed, st.Install.State)
	assert.Contains(t, st.Install.Error, "SHA-256")
	assert.False(t, st.Install.Cancelled)
	assert.False(t, st.Installed)
	assert.Empty(t, s.Dirs(), "a download that does not match is never used")
	_, statErr := os.Stat(r.ins.Layout.BinDir())
	assert.Error(t, statErr)
}

func TestServiceCancelIsNotAFailureAndInstallingAgainResumes(t *testing.T) {
	r := newRig(t, "9.9.9")
	body := r.bodies["/dl/9.9.9/ffmpeg.zip"]
	gate := make(chan struct{})
	first := true
	r.onServe = func(w http.ResponseWriter, req *http.Request) bool {
		if !first {
			return false
		}
		first = false
		w.Header().Set("Content-Length", "100000000")
		_, _ = w.Write(body[:10])
		w.(http.Flusher).Flush()
		close(gate)
		<-req.Context().Done()
		return true
	}
	s := newService(t, r)
	started, err := s.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	<-gate
	s.CancelInstall()
	s.WaitInstall()
	st := s.Status()
	assert.Equal(t, ffmpegdl.InstallFailed, st.Install.State)
	assert.True(t, st.Install.Cancelled)
	assert.Empty(t, st.Install.Error, "pressing cancel is not an error")
	assert.False(t, st.Installed)

	started, err = s.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	s.WaitInstall()
	assert.Equal(t, ffmpegdl.InstallSucceeded, s.Status().Install.State, s.Status().Install.Error)
	assert.True(t, s.Status().Installed)
}

func TestServiceRefusesAtOnceWhenTheDirectoryIsNotWritableOrTheDiskIsFull(t *testing.T) {
	r := newRig(t, "9.9.9")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	r.ins.Layout = ffmpegdl.Layout{Root: filepath.Join(file, "ffmpeg")}
	s := newService(t, r)
	started, err := s.StartInstall()
	require.ErrorIs(t, err, exeinstall.ErrNotWritable)
	assert.False(t, started)
	assert.Equal(t, ffmpegdl.InstallIdle, s.Status().Install.State, "a refusal leaves the state as it was")
	s.WaitInstall() // returns at once: nothing was started

	r = newRig(t, "9.9.9")
	s = ffmpegdl.New(ffmpegdl.Options{
		Layout: r.ins.Layout, GOOS: "linux", GOARCH: "amd64", FreeBytes: func(string) (uint64, error) { return 1, nil },
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): r.ins.Platform}},
	})
	var noSpace *fetch.NoSpaceError
	_, err = s.StartInstall()
	require.ErrorAs(t, err, &noSpace)
}

func TestServiceOnAPlatformWithNoBuildOffersNothing(t *testing.T) {
	r := newRig(t, "9.9.9")
	for name, o := range map[string]ffmpegdl.Options{
		"another platform": {Layout: r.ins.Layout, Catalog: ffmpegdl.Default(), GOOS: "linux", GOARCH: "amd64"},
		"no directory":     {Catalog: ffmpegdl.Default(), GOOS: "windows", GOARCH: "amd64"},
	} {
		t.Run(name, func(t *testing.T) {
			s := ffmpegdl.New(o)
			assert.False(t, s.Status().Supported)
			assert.Empty(t, s.Dirs())
			started, err := s.StartInstall()
			require.ErrorIs(t, err, ffmpegdl.ErrUnsupported)
			assert.False(t, started)
			require.ErrorIs(t, s.Remove(), ffmpegdl.ErrUnsupported)
			s.CancelInstall()
			s.WaitInstall()
			s.Close()
		})
	}
}

func TestServiceCloseStopsAnInstallAndRefusesANewOne(t *testing.T) {
	r := newRig(t, "9.9.9")
	reached := make(chan struct{}, 1)
	r.onServe = func(_ http.ResponseWriter, req *http.Request) bool {
		select {
		case reached <- struct{}{}:
		default:
		}
		<-req.Context().Done()
		return true
	}
	s := newService(t, r)
	_, err := s.StartInstall()
	require.NoError(t, err)
	<-reached
	s.Close()
	assert.Equal(t, ffmpegdl.InstallFailed, s.Status().Install.State)
	_, err = s.StartInstall()
	require.Error(t, err)
}

func TestExplainSaysWhatToDoForEachFailure(t *testing.T) {
	for err, want := range map[error]string{
		fetch.ErrChecksum:             "SHA-256",
		fetch.ErrSize:                 "SHA-256",
		exeinstall.ErrMismatch:        "中身",
		exeinstall.ErrUnsafeArchive:   "中身",
		&fetch.HTTPError{Status: 404}: "更新してください",
		&fetch.HTTPError{Status: 503}: "しばらくしてから",
		context.DeadlineExceeded:      "入れられませんでした",
	} {
		assert.Contains(t, ffmpegdl.Explain(err), want)
	}
}

func TestServiceRefusesToStartAnInstallWhileTheDownloadedCopyIsBeingRemoved(t *testing.T) {
	r := newRig(t, "9.9.9")
	s := newService(t, r)
	started, err := s.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	s.WaitInstall()
	require.True(t, s.Status().Installed)

	inRemoval, release := make(chan struct{}), make(chan struct{})
	ffmpegdl.HoldRemoval(s, func() error {
		close(inRemoval)
		<-release
		return nil
	})
	removed := make(chan error, 1)
	go func() { removed <- s.Remove() }()
	<-inRemoval

	// The files are being deleted: an install that started now would run on a directory that disappears under it.
	started, err = s.StartInstall()
	require.ErrorIs(t, err, ffmpegdl.ErrBusy)
	assert.False(t, started)
	assert.Equal(t, ffmpegdl.InstallIdle, s.Status().Install.State, "nothing was started")
	require.ErrorIs(t, s.Remove(), ffmpegdl.ErrBusy, "two removals do not overlap either")

	close(release)
	require.NoError(t, <-removed)
	started, err = s.StartInstall()
	require.NoError(t, err, "installing is possible again once the removal is over")
	assert.True(t, started)
	s.WaitInstall()
	assert.Equal(t, ffmpegdl.InstallSucceeded, s.Status().Install.State)
}

func TestNewPutsBackWhatAnInterruptedSwapLeft(t *testing.T) {
	r := newRig(t, "9.9.9")
	lay := r.ins.Layout
	// The swap stopped between its two renames: the installed copy is only bin.old, and a half-extracted bin.new is there.
	require.NoError(t, os.MkdirAll(lay.BinOldDir(), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(lay.BinOldDir(), "ffmpeg"), []byte(fakeFFmpeg), 0o700))
	require.NoError(t, os.MkdirAll(lay.BinNewDir(), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(lay.BinNewDir(), "ffmpeg"), []byte("half"), 0o700))

	newService(t, r)

	restored, err := os.ReadFile(filepath.Join(lay.BinDir(), "ffmpeg"))
	require.NoError(t, err, "the previous copy is back in place at startup")
	assert.Equal(t, fakeFFmpeg, string(restored))
	for _, gone := range []string{lay.BinOldDir(), lay.BinNewDir()} {
		_, statErr := os.Stat(gone)
		assert.Error(t, statErr, "%s is cleaned at startup", gone)
	}
}
