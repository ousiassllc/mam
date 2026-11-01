package runtime_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

func zipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range sortedKeys(files) {
		h, err := w.Create(name)
		require.NoError(t, err)
		_, err = h.Write([]byte(files[name]))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := range keys { // a handful of names: insertion sort keeps the archive's order stable
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// parts is a fake of the hosts of an assembled runtime: the embeddable Python, the vendor's source and two wheels.
type assembled struct {
	*rtm.Installer
	srv   *httptest.Server
	mu    sync.Mutex
	seen  []string
	bodys map[string][]byte
}

func (a *assembled) Seen() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.seen...)
}

func newAssembled(t *testing.T) *assembled {
	t.Helper()
	a := &assembled{bodys: map[string][]byte{
		"/python.zip": zipFiles(t, map[string]string{"python.exe": "MZ", "python310.zip": "z", "python310._pth": "python310.zip\n."}),
		"/musetalk.zip": zipFiles(t, map[string]string{
			"MuseTalk-abc/":                       "",
			"MuseTalk-abc/scripts/inference.py":   "print('v1.5')",
			"MuseTalk-abc/musetalk/__init__.py":   "",
			"MuseTalk-abc/configs/inference.yaml": "a: 1",
		}),
		"/torch.whl": zipFiles(t, map[string]string{"torch/__init__.py": "import ctypes", "torch-2.0.1.dist-info/METADATA": "Name: torch", "torch-2.0.1.data/scripts/torchrun": "x"}),
		"/numpy.whl": zipFiles(t, map[string]string{"numpy/__init__.py": "", "numpy-1.23.5.dist-info/METADATA": "Name: numpy"}),
	}}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.seen = append(a.seen, r.URL.Path)
		a.mu.Unlock()
		body, ok := a.bodys[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "x.zip", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(a.srv.Close)

	mk := func(name, kind, path string, extra func(*parts.Part)) parts.Part {
		body := a.bodys[path]
		sum := sha256.Sum256(body)
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		require.NoError(t, err)
		var ext int64
		for _, f := range zr.File {
			ext += int64(f.UncompressedSize64) //nolint:gosec // a test archive
		}
		p := parts.Part{Name: name, Kind: kind, URL: a.srv.URL + path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body)), ExtractedSize: ext}
		if extra != nil {
			extra(&p)
		}
		return p
	}
	ps := []parts.Part{
		mk("python", parts.KindZip, "/python.zip", func(p *parts.Part) { p.Dest = "python" }),
		mk("musetalk", parts.KindZip, "/musetalk.zip", func(p *parts.Part) { p.Strip = "MuseTalk-abc" }),
		mk("torch", parts.KindWheel, "/torch.whl", nil),
		mk("numpy", parts.KindWheel, "/numpy.whl", nil),
	}
	var size, ext int64
	for _, p := range ps {
		size += p.Size
		ext += p.ExtractedSize
	}
	layout := store.ForRuntime(filepath.Join(t.TempDir(), "localai"), "musetalk")
	st, err := store.OpenRuntime(layout)
	require.NoError(t, err)
	a.Installer = &rtm.Installer{
		Layout: layout, Store: st, Extract: extract.SevenZip{},
		Overlays: map[string][]byte{"server.py": []byte("# the server of mam\n")},
		Runtime: catalog.Runtime{
			ID: "musetalk", Name: "MuseTalk 1.5", Version: "1.5-abc",
			Build: &parts.Build{
				SitePackages: "python/Lib/site-packages",
				Files: []parts.File{
					{Dest: "python/python310._pth", Lines: []string{"python310.zip", ".", "Lib/site-packages", "..", "import site"}},
					{Dest: "mam_musetalk_server.py", Overlay: "server.py"},
				},
			},
			Launch: catalog.RuntimeLaunch{
				Python: "python/python.exe", PathDirs: []string{"python"}, Entry: "mam_musetalk_server.py",
				Args:      []string{"-I", "-u", "mam_musetalk_server.py", "--host", "127.0.0.1", "--port", "{port}"},
				ReadyPath: "/health", Env: []string{"HF_HUB_OFFLINE=1"}, StartTimeoutSec: 60,
			},
			Variants: map[catalog.Variant]catalog.RuntimeVariant{catalog.VariantNVIDIA: {
				Asset: catalog.Asset{Label: "test", Size: size, ExtractedSize: ext}, Parts: ps,
			}},
		},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
		Getenv:    func(string) string { return "/usr/bin" },
	}
	return a
}

func TestInstallPutsTheEnvironmentTogetherFromItsParts(t *testing.T) {
	a := newAssembled(t)
	var phases []rtm.Phase
	var lastDone, lastTotal int64
	require.NoError(t, a.Install(context.Background(), catalog.VariantNVIDIA, func(p rtm.Phase, done, total int64) {
		if len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
		if p == rtm.PhaseExtract {
			lastDone, lastTotal = done, total
		}
	}))
	assert.Equal(t, []rtm.Phase{rtm.PhaseDownload, rtm.PhaseExtract}, phases)
	assert.Equal(t, lastTotal, lastDone, "the extraction reports it is whole")

	env := a.Layout.EnvDir()
	for _, want := range []string{
		"python/python.exe",                            // the embeddable Python, in its own directory
		"scripts/inference.py", "musetalk/__init__.py", // the vendor's tree at the root, without its top directory
		"python/Lib/site-packages/torch/__init__.py", "python/Lib/site-packages/torch-2.0.1.dist-info/METADATA", "python/Lib/site-packages/numpy/__init__.py",
		"mam_musetalk_server.py",
	} {
		assert.FileExists(t, filepath.Join(env, filepath.FromSlash(want)), want)
	}
	assert.NoFileExists(t, filepath.Join(env, "python", "Lib", "site-packages", "torch-2.0.1.data", "scripts", "torchrun"))
	assert.NoDirExists(t, filepath.Join(env, "MuseTalk-abc"))
	b, err := os.ReadFile(filepath.Join(env, "python", "python310._pth"))
	require.NoError(t, err)
	assert.Equal(t, "python310.zip\n.\nLib/site-packages\n..\nimport site\n", string(b), "the path file of the build replaces the one of the Python")
	b, err = os.ReadFile(filepath.Join(env, "mam_musetalk_server.py"))
	require.NoError(t, err)
	assert.Equal(t, "# the server of mam\n", string(b))

	st := a.Status()
	assert.True(t, st.Installed)
	assert.Equal(t, "1.5-abc", st.Version)
	assert.NoDirExists(t, a.Layout.DownloadsDir(), "the downloaded parts are deleted once the environment stands")
	assert.NoDirExists(t, a.Layout.EnvNewDir())
	assert.ElementsMatch(t, []string{"/python.zip", "/musetalk.zip", "/torch.whl", "/numpy.whl"}, a.Seen())
}

// The path file of the embeddable Python is in the Python's own archive, and the build's file replaces it: the order of the steps is
// part of the contract, so the build file wins whatever the archive holds.
func TestAssembledBuildFileOverridesWhatAPartHolds(t *testing.T) {
	a := newAssembled(t)
	require.NoError(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil))
	b, err := os.ReadFile(filepath.Join(a.Layout.EnvDir(), "python", "python310._pth"))
	require.NoError(t, err)
	assert.Equal(t, "python310.zip\n.\nLib/site-packages\n..\nimport site\n", string(b))
}

func TestAssembledInstallRefusesAPartWithAWrongChecksumAndKeepsNothingOfIt(t *testing.T) {
	a := newAssembled(t)
	v := a.Runtime.Variants[catalog.VariantNVIDIA]
	v.Parts[3].SHA256 = strings.Repeat("0", 64) // numpy's wheel is not the pinned one
	require.ErrorIs(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil), fetch.ErrChecksum)
	assert.False(t, a.Status().Installed)
	assert.NoDirExists(t, a.Layout.EnvDir())
	assert.NoFileExists(t, fetch.PartPath(filepath.Join(a.Layout.DownloadsDir(), "numpy.zip")))
	assert.NoFileExists(t, filepath.Join(a.Layout.DownloadsDir(), "numpy.zip"))
	assert.FileExists(t, filepath.Join(a.Layout.DownloadsDir(), "python.zip"), "the parts before it stay for the next run")
}

// A part that an interrupted install downloaded whole is not fetched again.
func TestAssembledInstallResumesFromTheParts(t *testing.T) {
	a := newAssembled(t)
	v := a.Runtime.Variants[catalog.VariantNVIDIA]
	good := v.Parts[3].SHA256
	v.Parts[3].SHA256 = strings.Repeat("0", 64)
	require.Error(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil))
	first := len(a.Seen())

	need, err := a.NeedBytes(catalog.VariantNVIDIA)
	require.NoError(t, err)
	assert.Equal(t, v.ExtractedSize+v.Parts[3].Size, need, "only the part that is not there yet needs room for its download")

	v.Parts[3].SHA256 = good
	require.NoError(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil))
	assert.True(t, a.Status().Installed)
	assert.Equal(t, []string{"/numpy.whl"}, a.Seen()[first:], "the parts that were downloaded are verified in place and not requested again")
}

func TestAssembledInstallRefusesAPartThatHoldsMoreThanThePin(t *testing.T) {
	a := newAssembled(t)
	v := a.Runtime.Variants[catalog.VariantNVIDIA]
	v.Parts[2].ExtractedSize-- // the wheel holds more than the catalog says
	err := a.Install(context.Background(), catalog.VariantNVIDIA, nil)
	require.ErrorIs(t, err, extract.ErrTooLarge)
	assert.False(t, a.Status().Installed)
	assert.NoDirExists(t, a.Layout.EnvNewDir(), "a half-built environment is removed")
	assert.NoDirExists(t, a.Layout.EnvDir())
}

func TestAssembledInstallKeepsTheCurrentEnvironmentWhenAPartIsBad(t *testing.T) {
	a := newAssembled(t)
	require.NoError(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil))
	a.Runtime.Version = "1.5-def"
	v := a.Runtime.Variants[catalog.VariantNVIDIA]
	v.Parts[2].ExtractedSize--
	require.ErrorIs(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil), extract.ErrTooLarge)
	assert.True(t, a.Status().Installed)
	assert.Equal(t, "1.5-abc", a.Store.Get().Installed.Version)
	assert.FileExists(t, filepath.Join(a.Layout.EnvDir(), "python", "python.exe"))
}

func TestAssembledInstallRefusesWhenTheBuildWritesAnOverlayThatIsNotThere(t *testing.T) {
	a := newAssembled(t)
	a.Overlays = nil
	require.ErrorContains(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil), "no overlay")
	assert.False(t, a.Status().Installed)
}

// The parts come from the package hosts and the default URL check lets those through; the weights' check does not.
func TestAssembledInstallUsesThePackageHostsCheckByDefault(t *testing.T) {
	a := newAssembled(t)
	a.Fetch.CheckURL = nil
	err := a.Install(context.Background(), catalog.VariantNVIDIA, nil)
	require.Error(t, err, "127.0.0.1 is not a package host, whatever the catalog says")
	assert.Empty(t, a.Seen(), "no request was sent")
}

func TestAssembledStartIsTheEmbeddedPythonWithTheServerScript(t *testing.T) {
	a := newAssembled(t)
	require.NoError(t, a.Install(context.Background(), catalog.VariantNVIDIA, nil))
	spec, err := a.Command(40123)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(a.Layout.EnvDir(), "python", "python.exe"), spec.Command)
	assert.Equal(t, []string{"-I", "-u", "mam_musetalk_server.py", "--host", "127.0.0.1", "--port", "40123"}, spec.Args)
	assert.Equal(t, a.Layout.EnvDir(), spec.Dir, "the working directory is the MuseTalk tree: its relative paths (models/...) resolve there")
}
