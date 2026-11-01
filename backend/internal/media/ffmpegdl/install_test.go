package ffmpegdl_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func sum(b string) string { h := sha256.Sum256([]byte(b)); return hex.EncodeToString(h[:]) }

// zipOf is a zip with the given entries (name, content), in order.
func zipOf(t *testing.T, entries ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		require.NoError(t, err)
		_, err = w.Write([]byte(e[1]))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// rig is an Installer over a test server that serves the archives of a pinned platform. The platform is built from the archives
// as they are delivered, so every checksum is right until a test changes one.
type rig struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	bodies  map[string][]byte // by URL path: what the server delivers
	onServe func(w http.ResponseWriter, r *http.Request) bool
	ins     *ffmpegdl.Installer
}

const (
	fakeFFmpeg  = "#!/bin/sh\necho 'ffmpeg version 9.9.9 fake'\n"
	fakeFFprobe = "#!/bin/sh\necho 'ffprobe version 9.9.9 fake'\n"
)

// newRig serves one archive with both executables inside a directory, like the Windows build.
func newRig(t *testing.T, version string) *rig {
	t.Helper()
	body := zipOf(t,
		[2]string{"build/", ""},
		[2]string{"build/bin/ffmpeg", fakeFFmpeg},
		[2]string{"build/bin/ffprobe", fakeFFprobe},
		[2]string{"build/README.txt", "docs"},
	)
	r := &rig{t: t, bodies: map[string][]byte{"/dl/" + version + "/ffmpeg.zip": body}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		hook, b, ok := r.onServe, r.bodies[req.URL.Path], false
		_, ok = r.bodies[req.URL.Path]
		r.mu.Unlock()
		if hook != nil && hook(w, req) {
			return
		}
		if !ok {
			http.NotFound(w, req)
			return
		}
		http.ServeContent(w, req, "ffmpeg.zip", time.Time{}, bytes.NewReader(b))
	}))
	t.Cleanup(r.srv.Close)
	r.ins = &ffmpegdl.Installer{
		Layout: ffmpegdl.Layout{Root: filepath.Join(t.TempDir(), "ffmpeg")},
		Platform: ffmpegdl.Platform{Version: version, Archives: []ffmpegdl.Archive{{
			URL: r.srv.URL + "/dl/" + version + "/ffmpeg.zip", Format: "zip", SHA256: sum(string(body)), Size: int64(len(body)),
			Members: []ffmpegdl.Member{
				{Entry: "build/bin/ffmpeg", Name: "ffmpeg", Size: int64(len(fakeFFmpeg)), SHA256: sum(fakeFFmpeg)},
				{Entry: "build/bin/ffprobe", Name: "ffprobe", Size: int64(len(fakeFFprobe)), SHA256: sum(fakeFFprobe)},
			},
		}}},
		Fetch: fetch.Options{Client: r.srv.Client(), CheckURL: func(*url.URL) error { return nil }},
		// The fake executables are shell scripts: the default check (-version) would run them, which only the tests of the
		// check itself do.
		Check: func(context.Context, string) error { return nil },
	}
	return r
}

func (r *rig) archivePath() string {
	return filepath.Join(r.ins.Layout.DownloadsDir(), "ffmpeg.zip")
}

// nothingInstalled asserts that nothing is installed and nothing is left behind.
func (r *rig) nothingInstalled() {
	r.t.Helper()
	assert.False(r.t, r.ins.Status().Installed)
	assert.Empty(r.t, r.ins.Dirs(), "nothing is offered to the search")
	for _, gone := range []string{r.ins.Layout.BinDir(), r.ins.Layout.BinNewDir(), r.ins.Layout.BinOldDir(), r.ins.Layout.RecordPath(), r.archivePath(), fetch.PartPath(r.archivePath())} {
		_, err := os.Stat(gone)
		assert.Error(r.t, err, "%s is left behind", gone)
	}
}

func TestInstallDownloadsVerifiesExtractsChecksAndRecords(t *testing.T) {
	r := newRig(t, "9.9.9")
	var mu sync.Mutex
	var phases []ffmpegdl.Phase
	var checked []string
	r.ins.Check = func(_ context.Context, exe string) error {
		checked = append(checked, filepath.Base(filepath.Dir(exe))+"/"+filepath.Base(exe))
		return nil
	}
	err := r.ins.Install(context.Background(), func(p ffmpegdl.Phase, done, total int64) {
		mu.Lock()
		defer mu.Unlock()
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
		assert.LessOrEqual(t, done, total)
	})
	require.NoError(t, err)
	assert.Equal(t, []ffmpegdl.Phase{ffmpegdl.PhaseDownload, ffmpegdl.PhaseExtract, ffmpegdl.PhaseCheck}, phases)
	assert.Equal(t, []string{"bin.new/ffmpeg", "bin.new/ffprobe"}, checked, "each executable is checked where it was extracted, before it is put in place")

	st := r.ins.Status()
	assert.True(t, st.Installed)
	assert.Equal(t, "9.9.9", st.Version)
	assert.False(t, st.UpdateAvailable)
	assert.False(t, st.Broken)
	assert.Equal(t, []string{r.ins.Layout.BinDir()}, r.ins.Dirs())
	for name, want := range map[string]string{"ffmpeg": fakeFFmpeg, "ffprobe": fakeFFprobe} {
		got, err := os.ReadFile(filepath.Join(r.ins.Layout.BinDir(), name))
		require.NoError(t, err)
		assert.Equal(t, want, string(got))
	}
	entries, _ := os.ReadDir(r.ins.Layout.BinDir())
	assert.Len(t, entries, 2, "only the two executables, not the README or the directories of the archive")
	for _, gone := range []string{r.ins.Layout.DownloadsDir(), r.ins.Layout.BinNewDir(), r.ins.Layout.BinOldDir()} {
		_, err := os.Stat(gone)
		assert.Error(t, err, "%s is left behind", gone)
	}
}

func TestInstallOfTwoArchivesLikeTheMacOSBuild(t *testing.T) {
	r := newRig(t, "9.9.9")
	ffmpegZip := zipOf(t, [2]string{"ffmpeg", fakeFFmpeg})
	ffprobeZip := zipOf(t, [2]string{"ffprobe", fakeFFprobe})
	r.bodies["/dl/ffmpeg.zip"], r.bodies["/dl/ffprobe.zip"] = ffmpegZip, ffprobeZip
	r.ins.Platform.Archives = []ffmpegdl.Archive{
		{URL: r.srv.URL + "/dl/ffmpeg.zip", Format: "zip", SHA256: sum(string(ffmpegZip)), Size: int64(len(ffmpegZip)),
			Members: []ffmpegdl.Member{{Entry: "ffmpeg", Name: "ffmpeg", Size: int64(len(fakeFFmpeg)), SHA256: sum(fakeFFmpeg)}}},
		{URL: r.srv.URL + "/dl/ffprobe.zip", Format: "zip", SHA256: sum(string(ffprobeZip)), Size: int64(len(ffprobeZip)),
			Members: []ffmpegdl.Member{{Entry: "ffprobe", Name: "ffprobe", Size: int64(len(fakeFFprobe)), SHA256: sum(fakeFFprobe)}}},
	}
	require.NoError(t, r.ins.Install(context.Background(), nil))
	assert.True(t, r.ins.Status().Installed)
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		_, err := os.Stat(filepath.Join(r.ins.Layout.BinDir(), n))
		assert.NoError(t, err)
	}
}

func TestAWrongArchiveChecksumInstallsNothingAndLeavesNothing(t *testing.T) {
	r := newRig(t, "9.9.9")
	r.ins.Platform.Archives[0].SHA256 = strings.Repeat("0", 64)
	err := r.ins.Install(context.Background(), nil)
	require.ErrorIs(t, err, fetch.ErrChecksum)
	r.nothingInstalled()
	assert.Contains(t, ffmpegdl.Explain(err), "SHA-256", "the user is told what happened")
}

func TestAnArchiveOfAnotherSizeInstallsNothing(t *testing.T) {
	r := newRig(t, "9.9.9")
	r.ins.Platform.Archives[0].Size-- // the server delivers more than was pinned
	require.Error(t, r.ins.Install(context.Background(), nil))
	r.nothingInstalled()
}

func TestAnArchiveWhoseExecutableIsNotThePinnedOneInstallsNothing(t *testing.T) {
	// The archive matches its pinned SHA-256 (a build that was reviewed), but the ffmpeg in it is not the one the catalog says.
	r := newRig(t, "9.9.9")
	r.ins.Platform.Archives[0].Members[0].SHA256 = sum("another ffmpeg of the same size........................")
	r.ins.Platform.Archives[0].Members[0].Size = int64(len(fakeFFmpeg))
	err := r.ins.Install(context.Background(), nil)
	require.ErrorIs(t, err, exeinstall.ErrMismatch)
	r.nothingInstalled()
}

func TestAnArchiveWithoutTheExecutableInstallsNothing(t *testing.T) {
	r := newRig(t, "9.9.9")
	r.ins.Platform.Archives[0].Members[1].Entry = "build/bin/missing"
	require.ErrorIs(t, r.ins.Install(context.Background(), nil), exeinstall.ErrUnsafeArchive)
	r.nothingInstalled()
}

func TestAnExecutableThatDoesNotRunInstallsNothing(t *testing.T) {
	r := newRig(t, "9.9.9")
	r.ins.Check = func(context.Context, string) error { return errors.New("exec format error") }
	err := r.ins.Install(context.Background(), nil)
	require.ErrorIs(t, err, ffmpegdl.ErrNotRunnable)
	r.nothingInstalled()
	assert.Contains(t, ffmpegdl.Explain(err), "実行できませんでした")
}

func TestAServerErrorInstallsNothing(t *testing.T) {
	r := newRig(t, "9.9.9")
	delete(r.bodies, "/dl/9.9.9/ffmpeg.zip")
	err := r.ins.Install(context.Background(), nil)
	var herr *fetch.HTTPError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, http.StatusNotFound, herr.Status)
	r.nothingInstalled()
	assert.Contains(t, ffmpegdl.Explain(err), "更新してください")
}

func TestOnlyDownloadHostsAreAcceptedByDefault(t *testing.T) {
	r := newRig(t, "9.9.9")
	r.ins.Fetch.CheckURL = nil
	err := r.ins.Install(context.Background(), nil)
	require.Error(t, err, "the test server is not a download host")
	r.nothingInstalled()
}
