package ffmpegdl_test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// realBuild is a zip of the machine's own ffmpeg and ffprobe, and the platform that pins it, served by a test server: the
// stand-in of the download of a real build. Its executables really run, so what is found after the install can be used.
func realBuild(t *testing.T) (platform ffmpegdl.Platform, srv *httptest.Server) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the machine's ffmpeg is looked up by its Windows name there; the Linux and macOS runs cover the lookup")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var members []ffmpegdl.Member
	for _, name := range []string{locate.FFmpeg, locate.FFprobe} {
		exe, err := locate.Find(name, "")
		if err != nil {
			testenv.MissingFFmpeg(t, name+" is not installed")
		}
		b, err := os.ReadFile(exe) //nolint:gosec // the machine's own ffmpeg
		require.NoError(t, err)
		w, err := zw.Create("build/bin/" + name)
		require.NoError(t, err)
		_, err = w.Write(b)
		require.NoError(t, err)
		members = append(members, ffmpegdl.Member{Entry: "build/bin/" + name, Name: name, Size: int64(len(b)), SHA256: sum(string(b))})
	}
	require.NoError(t, zw.Close())
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "ffmpeg.zip", time.Time{}, bytes.NewReader(buf.Bytes()))
	}))
	t.Cleanup(srv.Close)
	return ffmpegdl.Platform{Version: "9.9.9", Archives: []ffmpegdl.Archive{{
		URL: srv.URL + "/ffmpeg.zip", Format: "zip", SHA256: sum(buf.String()), Size: int64(buf.Len()), Members: members,
	}}}, srv
}

// A machine with no ffmpeg on PATH: the lookup of the app (ffmpegsettings, which Judge, the Take previews and Export are
// built from) finds nothing, then finds the downloaded copy, and the tools it returns work.
func TestAfterTheDownloadTheAppFindsFFmpegAndTheToolsWork(t *testing.T) {
	platform, srv := realBuild(t)
	service := ffmpegdl.New(ffmpegdl.Options{
		Layout:  ffmpegdl.Layout{Root: filepath.Join(t.TempDir(), "ffmpeg")},
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): platform}},
		GOOS:    "linux", GOARCH: "amd64",
		Fetch: fetch.Options{Client: srv.Client(), CheckURL: func(*url.URL) error { return nil }},
	}) // the default check: each executable really runs with -version
	t.Cleanup(service.Close)
	noPath := locate.Finder{GOOS: runtime.GOOS, Path: "", Usable: locate.IsExecutable}
	settings := ffmpegsettings.New(filepath.Join(t.TempDir(), ffmpegsettings.FileName), ffmpegsettings.WithFinder(noPath), ffmpegsettings.WithManagedDirs(service.Dirs))

	_, err := settings.Look()
	require.ErrorIs(t, err, media.ErrUnavailable, "no ffmpeg on the machine")

	started, err := service.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	service.WaitInstall()
	require.Equal(t, ffmpegdl.InstallSucceeded, service.Status().Install.State, service.Status().Install.Error)

	tools, err := settings.Look()
	require.NoError(t, err, "found by the lookup the app uses at its start")
	v, err := settings.View()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(service.Status().Dir, "ffmpeg"), v.FFmpeg.Path)
	assert.Equal(t, filepath.Join(service.Status().Dir, "ffprobe"), v.FFprobe.Path)

	// Probe (ffprobe) and Thumbnail (ffmpeg): what the Judge, the previews and Export call.
	dir := t.TempDir()
	video := filepath.Join(dir, "a.mp4")
	require.NoError(t, os.WriteFile(video, testenv.PatternVideo(t, "160x120", 1), 0o600))
	info, err := tools.Probe(context.Background(), video)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, info.Duration, 0.3)
	thumb := filepath.Join(dir, "t.jpg")
	require.NoError(t, tools.Thumbnail(context.Background(), video, thumb, 0))
	st, err := os.Stat(thumb)
	require.NoError(t, err)
	assert.Positive(t, st.Size())
}

// A download that does not match its pinned SHA-256 is never used: the lookup still finds nothing.
func TestAMismatchedDownloadIsNeverFound(t *testing.T) {
	platform, srv := realBuild(t)
	platform.Archives[0].SHA256 = strings.Repeat("a", 64)
	service := ffmpegdl.New(ffmpegdl.Options{
		Layout:  ffmpegdl.Layout{Root: filepath.Join(t.TempDir(), "ffmpeg")},
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): platform}},
		GOOS:    "linux", GOARCH: "amd64",
		Fetch: fetch.Options{Client: srv.Client(), CheckURL: func(*url.URL) error { return nil }},
	})
	t.Cleanup(service.Close)
	settings := ffmpegsettings.New(filepath.Join(t.TempDir(), ffmpegsettings.FileName),
		ffmpegsettings.WithFinder(locate.Finder{GOOS: runtime.GOOS, Usable: locate.IsExecutable}), ffmpegsettings.WithManagedDirs(service.Dirs))

	_, err := service.StartInstall()
	require.NoError(t, err)
	service.WaitInstall()
	st := service.Status()
	assert.Equal(t, ffmpegdl.InstallFailed, st.Install.State)
	assert.NotEmpty(t, st.Install.Error)
	_, err = settings.Look()
	require.ErrorIs(t, err, media.ErrUnavailable)
	_, err = os.Stat(st.Dir)
	assert.Error(t, err, "not even a directory of unverified executables")
}

// A user's own ffmpeg on PATH is used, not the downloaded copy.
func TestAnFFmpegOnPathWinsOverTheDownloadedCopy(t *testing.T) {
	platform, srv := realBuild(t)
	service := ffmpegdl.New(ffmpegdl.Options{
		Layout:  ffmpegdl.Layout{Root: filepath.Join(t.TempDir(), "ffmpeg")},
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): platform}},
		GOOS:    "linux", GOARCH: "amd64",
		Fetch: fetch.Options{Client: srv.Client(), CheckURL: func(*url.URL) error { return nil }},
	})
	t.Cleanup(service.Close)
	_, err := service.StartInstall()
	require.NoError(t, err)
	service.WaitInstall()
	require.True(t, service.Status().Installed)

	mine := t.TempDir()
	for _, name := range []string{locate.FFmpeg, locate.FFprobe} {
		real, err := locate.Find(name, "")
		require.NoError(t, err)
		require.NoError(t, os.Symlink(real, filepath.Join(mine, name)))
	}
	finder := locate.Finder{GOOS: runtime.GOOS, Path: mine, Usable: locate.IsExecutable}
	settings := ffmpegsettings.New(filepath.Join(t.TempDir(), ffmpegsettings.FileName), ffmpegsettings.WithFinder(finder), ffmpegsettings.WithManagedDirs(service.Dirs))
	v, err := settings.View()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(mine, "ffmpeg"), v.FFmpeg.Path)
	assert.Equal(t, filepath.Join(mine, "ffprobe"), v.FFprobe.Path)
}
