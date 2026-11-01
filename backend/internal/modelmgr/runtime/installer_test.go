package runtime_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// archivePath is the synthetic 7z of the extract tests (LZMA2 + BCJ2 like the vendor's package): ComfyUI_windows_portable/
// {<python dir>/python.exe, ComfyUI/main.py, ...}. It stands in for the runtime package: the installer only needs a root, an
// interpreter and the entry script in it.
const archivePath = "../extract/testdata/bcj2.7z"

const (
	pyDir     = "python_embeded" //nolint:misspell // the directory name of the upstream package
	pythonRel = pyDir + "/python.exe"
	entryRel  = "ComfyUI/main.py"
)

// server is a fake host of the archive that records the requests (the Range of each).
type server struct {
	*httptest.Server
	body   []byte
	mu     sync.Mutex
	ranges []string
	// cutOnce makes the first request deliver half of the body and then drop the connection.
	cutOnce bool
}

func (s *server) Ranges() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

func newServer(t *testing.T) *server {
	t.Helper()
	body, err := os.ReadFile(archivePath)
	require.NoError(t, err)
	s := &server{body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.ranges = append(s.ranges, r.Header.Get("Range"))
		cut := s.cutOnce
		s.cutOnce = false
		s.mu.Unlock()
		if cut {
			w.Header().Set("Content-Length", strconv.Itoa(len(s.body)))
			_, _ = w.Write(s.body[:len(s.body)/2])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // the connection drops in the middle of the body
		}
		http.ServeContent(w, r, "a.7z", time.Time{}, strings.NewReader(string(s.body)))
	}))
	t.Cleanup(s.Close)
	return s
}

type rig struct {
	*rtm.Installer
	srv *server
}

func newRig(t *testing.T, version string) *rig {
	t.Helper()
	srv := newServer(t)
	layout := store.ForRuntime(filepath.Join(t.TempDir(), "localai"), "gpt-sovits")
	st, err := store.OpenRuntime(layout)
	require.NoError(t, err)
	sum := sha256.Sum256(srv.body)
	variant := func(minCC float64) catalog.RuntimeVariant {
		return catalog.RuntimeVariant{
			Asset: catalog.Asset{
				Label: "test", URL: srv.URL + "/GPT-SoVITS-test.7z", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(srv.body)), ExtractedSize: 1 << 20,
			},
			MinComputeCap: minCC,
		}
	}
	return &rig{srv: srv, Installer: &rtm.Installer{
		Layout: layout, Store: st, Extract: extract.SevenZip{},
		Runtime: catalog.Runtime{
			ID: "gpt-sovits", Name: "GPT-SoVITS", Version: version, ArchiveRoot: "ComfyUI_windows_portable",
			Launch: catalog.RuntimeLaunch{
				Python: pythonRel, PathDirs: []string{pyDir}, Entry: entryRel,
				Args: []string{"-I", "-u", entryRel, "-a", "127.0.0.1", "-p", "{port}"}, ReadyPath: "/openapi.json",
				Env: []string{"HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1"}, StartTimeoutSec: 60,
			},
			Variants: map[catalog.Variant]catalog.RuntimeVariant{catalog.VariantNVIDIA: variant(0), catalog.VariantNVIDIA50: variant(12)},
		},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
		Getenv:    func(string) string { return "/usr/bin" },
	}}
}

func TestInstallDownloadsVerifiesAndExtractsTheRuntime(t *testing.T) {
	r := newRig(t, "20250604")
	var phases []rtm.Phase
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, func(p rtm.Phase, _, _ int64) {
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
	}))
	assert.Equal(t, []rtm.Phase{rtm.PhaseDownload, rtm.PhaseExtract}, phases)

	st := r.Status()
	assert.True(t, st.Installed)
	assert.Equal(t, "20250604", st.Version)
	assert.Equal(t, "nvidia", st.Variant)
	assert.False(t, st.UpdateAvailable)
	assert.FileExists(t, r.PythonPath())
	assert.FileExists(t, filepath.Join(r.Layout.EnvDir(), entryRel))
	assert.NoDirExists(t, r.Layout.DownloadsDir(), "the archive is deleted once extracted")
	assert.NoDirExists(t, r.Layout.EnvNewDir())
	assert.Equal(t, "20250604", r.Store.Get().Installed.Version)
}

func TestInstallResumesAnInterruptedDownloadWithARangeRequest(t *testing.T) {
	r := newRig(t, "20250604")
	r.srv.cutOnce = true
	err := r.Install(context.Background(), catalog.VariantNVIDIA, nil)
	require.Error(t, err, "the connection dropped in the middle of the archive")
	assert.False(t, r.Status().Installed)
	part, statErr := os.Stat(fetch.PartPath(filepath.Join(r.Layout.DownloadsDir(), "GPT-SoVITS-test.7z")))
	require.NoError(t, statErr, "the part file is kept for the resume")
	assert.Positive(t, part.Size())

	need, err := r.NeedBytes(catalog.VariantNVIDIA)
	require.NoError(t, err)
	assert.Equal(t, int64(len(r.srv.body))-part.Size()+1<<20, need, "only what is not downloaded yet needs room")

	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	assert.True(t, r.Status().Installed)
	ranges := r.srv.Ranges()
	require.Len(t, ranges, 2)
	assert.Empty(t, ranges[0])
	assert.Equal(t, "bytes="+strconv.FormatInt(part.Size(), 10)+"-", ranges[1], "the second run continues where the first one stopped")
}

func TestInstallStopsWhenCancelledAndTheNextRunFinishes(t *testing.T) {
	r := newRig(t, "20250604")
	ctx, cancel := context.WithCancel(context.Background())
	err := r.Install(ctx, catalog.VariantNVIDIA, func(p rtm.Phase, done, _ int64) {
		if p == rtm.PhaseDownload && done > 0 {
			cancel()
		}
	})
	require.Error(t, err)
	assert.False(t, r.Status().Installed)
	assert.NoDirExists(t, r.Layout.EnvNewDir(), "a half-extracted environment is removed")

	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	assert.True(t, r.Status().Installed)
}

func TestInstallRefusesAWrongChecksumAndRemovesTheDownload(t *testing.T) {
	r := newRig(t, "20250604")
	v := r.Runtime.Variants[catalog.VariantNVIDIA]
	v.SHA256 = strings.Repeat("0", 64)
	r.Runtime.Variants[catalog.VariantNVIDIA] = v
	require.ErrorIs(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil), fetch.ErrChecksum)
	assert.False(t, r.Status().Installed)
	entries, _ := os.ReadDir(r.Layout.DownloadsDir())
	assert.Empty(t, entries, "neither the archive nor its part file is left")
	assert.NoDirExists(t, r.Layout.EnvDir())
}

func TestInstallRefusesAWrongSizeAndRemovesTheDownload(t *testing.T) {
	r := newRig(t, "20250604")
	v := r.Runtime.Variants[catalog.VariantNVIDIA]
	v.Size-- // the vendor's file is one byte longer than what the catalog pinned
	r.Runtime.Variants[catalog.VariantNVIDIA] = v
	err := r.Install(context.Background(), catalog.VariantNVIDIA, nil)
	require.ErrorIs(t, err, fetch.ErrSize)
	assert.False(t, r.Status().Installed)
	entries, _ := os.ReadDir(r.Layout.DownloadsDir())
	assert.Empty(t, entries)
}

func TestInstallStartsNothingWithoutFreeSpace(t *testing.T) {
	r := newRig(t, "20250604")
	r.FreeBytes = func(string) (uint64, error) { return uint64(len(r.srv.body)), nil } // room for the archive, not for its files
	err := r.Install(context.Background(), catalog.VariantNVIDIA, nil)
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns)
	assert.Equal(t, uint64(len(r.srv.body))+1<<20, ns.Need, "the archive and the extracted files")
	assert.Empty(t, r.srv.Ranges(), "no request was sent")

	_, err = r.NeedBytes("nope")
	assert.ErrorIs(t, err, rtm.ErrVariant)
	assert.ErrorIs(t, r.Install(context.Background(), "nope", nil), rtm.ErrVariant)
}

func TestInstallRefusesAPlaceThatCannotBeWritten(t *testing.T) {
	r := newRig(t, "20250604")
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	r.Layout = store.ForRuntime(blocker, "gpt-sovits") // a directory cannot be made below a file
	require.ErrorIs(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil), store.ErrNotWritable)
}

func TestUpdateReplacesTheEnvironment(t *testing.T) {
	r := newRig(t, "20250604")
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	require.NoError(t, os.WriteFile(filepath.Join(r.Layout.EnvDir(), "old-only.txt"), []byte("x"), 0o600))

	r.Runtime.Version = "20260101"
	st := r.Status()
	assert.True(t, st.Installed)
	assert.True(t, st.UpdateAvailable, "the catalog pins a newer version than the installed one")
	assert.Equal(t, "20250604", st.Version)
	assert.Equal(t, "20260101", st.PinnedVersion)

	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	assert.False(t, r.Status().UpdateAvailable)
	assert.NoFileExists(t, filepath.Join(r.Layout.EnvDir(), "old-only.txt"), "the environment is replaced as a whole")
	assert.NoDirExists(t, r.Layout.EnvOldDir())
}

// An archive that is not the runtime (no entry script) is refused and the environment that was there stays.
func TestInstallKeepsTheCurrentEnvironmentWhenTheNewArchiveIsNotTheRuntime(t *testing.T) {
	r := newRig(t, "20250604")
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	r.Runtime.Launch.Entry = "missing.py"
	r.Runtime.Version = "20260101"
	err := r.Install(context.Background(), catalog.VariantNVIDIA, nil)
	require.ErrorContains(t, err, "has no missing.py")
	assert.NoDirExists(t, r.Layout.EnvNewDir())
	assert.DirExists(t, r.Layout.EnvDir())
	assert.Equal(t, "20250604", r.Store.Get().Installed.Version)
}

func TestStatusNoticesDeletedFilesAndRemoveCleansEverythingButTheSettings(t *testing.T) {
	r := newRig(t, "20250604")
	assert.False(t, r.Status().Installed)
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))
	require.NoError(t, r.Store.Update(func(s *store.RuntimeState) { s.AutoStart = true }))
	require.NoError(t, os.MkdirAll(r.Layout.CacheDir(), 0o750))

	require.NoError(t, os.Remove(r.PythonPath()))
	st := r.Status()
	assert.False(t, st.Installed)
	assert.True(t, st.Broken)

	require.NoError(t, r.Remove())
	assert.NoDirExists(t, r.Layout.EnvDir())
	assert.NoDirExists(t, r.Layout.CacheDir())
	assert.Nil(t, r.Store.Get().Installed)
	assert.True(t, r.Store.Get().AutoStart, "the setting stays")
	assert.False(t, r.Status().Broken)
	require.NoError(t, r.Remove(), "removing what is not installed is not an error")
}

func TestCleanLeftovers(t *testing.T) {
	r := newRig(t, "20250604")
	l := r.Layout
	// The swap stopped after env -> env.old: the previous environment is put back.
	require.NoError(t, os.MkdirAll(filepath.Join(l.EnvOldDir(), pyDir), 0o750))
	require.NoError(t, os.MkdirAll(l.EnvNewDir(), 0o750))
	r.CleanLeftovers()
	assert.NoDirExists(t, l.EnvNewDir())
	assert.NoDirExists(t, l.EnvOldDir())
	assert.DirExists(t, filepath.Join(l.EnvDir(), pyDir))

	// env and env.old both exist: the new one won; the old one goes.
	require.NoError(t, os.MkdirAll(l.EnvOldDir(), 0o750))
	r.CleanLeftovers()
	assert.NoDirExists(t, l.EnvOldDir())
	assert.DirExists(t, l.EnvDir())
}

func TestCommandRunsTheVendorsServerOnLoopbackOnlyAndKeepsEverythingBelowTheRuntimeDirectory(t *testing.T) {
	r := newRig(t, "20250604")
	_, err := r.Command(9880)
	require.ErrorIs(t, err, rtm.ErrNotInstalled, "nothing is installed yet")
	require.NoError(t, r.Install(context.Background(), catalog.VariantNVIDIA, nil))

	spec, err := r.Command(51234)
	require.NoError(t, err)
	assert.Equal(t, r.PythonPath(), spec.Command)
	assert.Equal(t, r.Layout.EnvDir(), spec.Dir)
	assert.Equal(t, []string{"-I", "-u", entryRel, "-a", "127.0.0.1", "-p", "51234"}, spec.Args)
	assert.Equal(t, r.Layout.LogPath(), spec.LogPath)
	assert.NotContains(t, spec.Args, "0.0.0.0")

	env := map[string]string{}
	for _, kv := range spec.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	assert.Equal(t, filepath.Join(r.Layout.EnvDir(), pyDir)+string(os.PathListSeparator)+"/usr/bin", env["PATH"], "the runtime directory first, as the vendor's launcher does")
	assert.Equal(t, "1", env["HF_HUB_OFFLINE"], "the offline settings of the catalog")
	assert.Equal(t, "1", env["TRANSFORMERS_OFFLINE"])
	for _, k := range []string{"HF_HOME", "TORCH_HOME", "MODELSCOPE_CACHE", "XDG_CACHE_HOME", "TEMP", "TMP", "TMPDIR"} {
		assert.True(t, strings.HasPrefix(env[k], r.Layout.Root+string(os.PathSeparator)), "%s is below the runtime directory: %s", k, env[k])
	}
	assert.Subset(t, spec.DropEnv, []string{"PYTHONPATH", "PYTHONHOME"}, "the vendor's interpreter must not see the user's Python")
	assert.DirExists(t, r.Layout.CacheDir())
	assert.DirExists(t, r.Layout.TempDir())
}
