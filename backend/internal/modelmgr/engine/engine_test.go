package engine_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

const archivePath = "../extract/testdata/bcj2.7z"

type rig struct {
	inst *engine.Installer
	srv  *httptest.Server
	hits atomic.Int32
	body []byte
}

// newRig serves the synthetic portable archive (made with the upstream 7z flags) as the engine release asset.
// pythonExe is the path of the interpreter in the upstream package; the directory name is spelled that way upstream.
const (
	pyDir     = "python_embeded" //nolint:misspell // upstream spelling
	pythonExe = pyDir + "/python.exe"
)

func newRig(t *testing.T, version string) *rig {
	t.Helper()
	r := &rig{}
	var err error
	r.body, err = os.ReadFile(archivePath)
	require.NoError(t, err)
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits.Add(1)
		http.ServeContent(w, req, "a.7z", zeroTime, strings.NewReader(string(r.body))) // Range support
	}))
	t.Cleanup(r.srv.Close)
	l := store.Layout{Root: filepath.Join(t.TempDir(), "comfyui")}
	st, err := store.Open(l)
	require.NoError(t, err)
	sum := sha256.Sum256(r.body)
	r.inst = &engine.Installer{
		Layout: l, Store: st, Extract: extract.SevenZip{},
		Engine: catalog.Engine{
			Version: version, ArchiveRoot: "ComfyUI_windows_portable", Python: pythonExe, Main: "ComfyUI/main.py",
			Variants: map[catalog.Variant]catalog.Asset{
				catalog.VariantNVIDIA: {URL: r.srv.URL + "/ComfyUI_windows_portable_nvidia.7z", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(r.body)), ExtractedSize: 1 << 20},
			},
		},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
	}
	return r
}

func TestInstallDownloadsVerifiesAndExtractsTheEngine(t *testing.T) {
	r := newRig(t, "v0.42.1")
	var phases []engine.Phase
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, func(p engine.Phase, _, _ int64) {
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
	}))
	assert.Equal(t, []engine.Phase{engine.PhaseDownload, engine.PhaseExtract}, phases)

	st := r.inst.Status()
	assert.True(t, st.Installed)
	assert.Equal(t, "v0.42.1", st.Version)
	assert.Equal(t, "nvidia", st.Variant)
	assert.False(t, st.UpdateAvailable)
	assert.FileExists(t, r.inst.PythonPath())
	assert.FileExists(t, r.inst.MainPath())
	assert.NoDirExists(t, r.inst.Layout.DownloadsDir(), "the archive is deleted once extracted")
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir())
	assert.Equal(t, "v0.42.1", r.inst.Store.Get().Engine.Version)
}

func TestCommandIsLoopbackOnlyWithoutCustomNodesAndKeepsEverythingInTheDataDirectory(t *testing.T) {
	r := newRig(t, "v0.42.1")
	_, err := r.inst.Command(8188)
	require.Error(t, err, "nothing is installed yet")
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))

	spec, err := r.inst.Command(51234)
	require.NoError(t, err)
	assert.Equal(t, r.inst.PythonPath(), spec.Command)
	assert.Equal(t, r.inst.Layout.EngineDir(), spec.Dir)
	assert.Equal(t, []string{
		"-s", filepath.FromSlash("ComfyUI/main.py"),
		"--listen", "127.0.0.1", "--port", "51234",
		"--disable-auto-launch", "--disable-all-custom-nodes",
		"--base-directory", r.inst.Layout.DataDir(),
	}, spec.Args)
	assert.Equal(t, r.inst.Layout.LogPath(), spec.LogPath)
	assert.NotContains(t, spec.Args, "--cpu")
	assert.NotContains(t, spec.Args, "--windows-standalone-build")
	assert.DirExists(t, r.inst.Layout.DataDir())

	// CPU only when it was chosen at the install.
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, true, nil))
	spec, err = r.inst.Command(1)
	require.NoError(t, err)
	assert.Contains(t, spec.Args, "--cpu")
}

// A newer pinned version is installed over the old one; the data directory (models, outputs) is not touched.
func TestUpdateReplacesTheEngineAndKeepsTheData(t *testing.T) {
	r := newRig(t, "v0.42.1")
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))
	model := filepath.Join(r.inst.Layout.DataDir(), "models", "vae", "x.safetensors")
	require.NoError(t, os.MkdirAll(filepath.Dir(model), 0o750))
	require.NoError(t, os.WriteFile(model, []byte("weights"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(r.inst.Layout.EngineDir(), "old-only.txt"), []byte("x"), 0o600))

	r.inst.Engine.Version = "v0.43.0"
	st := r.inst.Status()
	assert.True(t, st.Installed)
	assert.True(t, st.UpdateAvailable, "the catalog pins a newer version than the installed one")
	assert.Equal(t, "v0.42.1", st.Version)
	assert.Equal(t, "v0.43.0", st.PinnedVersion)

	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))
	assert.False(t, r.inst.Status().UpdateAvailable)
	assert.Equal(t, "v0.43.0", r.inst.Status().Version)
	assert.NoFileExists(t, filepath.Join(r.inst.Layout.EngineDir(), "old-only.txt"), "the engine is replaced as a whole")
	assert.NoDirExists(t, r.inst.Layout.EngineOldDir())
	assert.FileExists(t, model)
}

func TestInstallResumesTheDownloadOfAnInterruptedInstall(t *testing.T) {
	r := newRig(t, "v0.42.1")
	ctx, cancel := context.WithCancel(context.Background())
	err := r.inst.Install(ctx, catalog.VariantNVIDIA, false, func(p engine.Phase, done, _ int64) {
		if p == engine.PhaseDownload && done > 0 {
			cancel()
		}
	})
	// The archive is tiny: the download may finish before the cancel is seen; then the extraction is what stops.
	require.Error(t, err)
	assert.False(t, r.inst.Status().Installed)
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir(), "a half-extracted engine is removed")

	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))
	assert.True(t, r.inst.Status().Installed)
}

func TestInstallRefusesAWrongChecksumAndRemovesTheDownload(t *testing.T) {
	r := newRig(t, "v0.42.1")
	a := r.inst.Engine.Variants[catalog.VariantNVIDIA]
	a.SHA256 = strings.Repeat("0", 64)
	r.inst.Engine.Variants[catalog.VariantNVIDIA] = a
	err := r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil)
	require.ErrorIs(t, err, fetch.ErrChecksum)
	assert.False(t, r.inst.Status().Installed)
	entries, _ := os.ReadDir(r.inst.Layout.DownloadsDir())
	assert.Empty(t, entries, "neither the archive nor its part file is left")
	assert.NoDirExists(t, r.inst.Layout.EngineDir())
}

func TestInstallStartsNothingWithoutFreeSpace(t *testing.T) {
	r := newRig(t, "v0.42.1")
	r.inst.FreeBytes = func(string) (uint64, error) { return uint64(len(r.body)), nil } // room for the archive, not for its files
	err := r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil)
	var ns *fetch.NoSpaceError
	require.ErrorAs(t, err, &ns)
	assert.Equal(t, uint64(len(r.body))+1<<20, ns.Need, "the archive and the extracted files")
	assert.Zero(t, r.hits.Load(), "no request was sent")

	need, err := r.inst.NeedBytes(catalog.VariantNVIDIA)
	require.NoError(t, err)
	assert.Equal(t, int64(len(r.body))+1<<20, need)
	_, err = r.inst.NeedBytes("nope")
	assert.ErrorIs(t, err, engine.ErrVariant)
	assert.ErrorIs(t, r.inst.Install(context.Background(), "nope", false, nil), engine.ErrVariant)
}

// An archive that is not the engine (no interpreter) is refused and the engine that was there stays.
func TestInstallKeepsTheCurrentEngineWhenTheNewArchiveIsNotAnEngine(t *testing.T) {
	r := newRig(t, "v0.42.1")
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))
	r.inst.Engine.Python = pyDir + "/missing.exe"
	r.inst.Engine.Version = "v0.43.0"
	err := r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no")
	assert.NoDirExists(t, r.inst.Layout.EngineNewDir())
	assert.DirExists(t, r.inst.Layout.EngineDir())
	assert.Equal(t, "v0.42.1", r.inst.Store.Get().Engine.Version)
}

func TestStatusNoticesDeletedFilesAndRemoveKeepsTheData(t *testing.T) {
	r := newRig(t, "v0.42.1")
	assert.False(t, r.inst.Status().Installed)
	require.NoError(t, r.inst.Install(context.Background(), catalog.VariantNVIDIA, false, nil))
	data := filepath.Join(r.inst.Layout.DataDir(), "keep.txt")
	require.NoError(t, os.MkdirAll(r.inst.Layout.DataDir(), 0o750))
	require.NoError(t, os.WriteFile(data, []byte("x"), 0o600))

	require.NoError(t, os.Remove(r.inst.PythonPath()))
	st := r.inst.Status()
	assert.False(t, st.Installed)
	assert.True(t, st.Broken)

	require.NoError(t, r.inst.Remove())
	assert.NoDirExists(t, r.inst.Layout.EngineDir())
	assert.Nil(t, r.inst.Store.Get().Engine)
	assert.False(t, r.inst.Status().Broken)
	assert.FileExists(t, data)
}

func TestCleanLeftovers(t *testing.T) {
	r := newRig(t, "v0.42.1")
	l := r.inst.Layout
	// The swap stopped after engine -> engine.old: the previous engine is put back.
	require.NoError(t, os.MkdirAll(filepath.Join(l.EngineOldDir(), pyDir), 0o750))
	require.NoError(t, os.MkdirAll(l.EngineNewDir(), 0o750))
	r.inst.CleanLeftovers()
	assert.NoDirExists(t, l.EngineNewDir())
	assert.NoDirExists(t, l.EngineOldDir())
	assert.DirExists(t, filepath.Join(l.EngineDir(), pyDir))

	// engine and engine.old both exist: the new engine won; the old one goes.
	require.NoError(t, os.MkdirAll(l.EngineOldDir(), 0o750))
	r.inst.CleanLeftovers()
	assert.NoDirExists(t, l.EngineOldDir())
	assert.DirExists(t, l.EngineDir())
}
