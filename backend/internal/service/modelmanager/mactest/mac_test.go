// Package mactest tests the bundled engine on an Apple silicon Mac (Issue #854): where it is supported, the one build it offers, where it is
// installed (the data directory, outside MAM.app) and that it starts and stops like the Windows one. The Mac is simulated: the Service is
// given the platform, the architecture and the macOS version, and installs the three kinds of parts (a Python archive, the ComfyUI source, a
// wheel) from a fake host; a stand-in process is the engine.
package mactest_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

const exe = "/Applications/MAM.app/Contents/MacOS/mam"

// parthost serves the parts of the macOS build under /<name> and records the paths asked for.
type parthost struct {
	*httptest.Server
	mu   sync.Mutex
	body map[string][]byte
	hits []string
}

// newParts builds the macOS build of the test catalog (apple.Parts have no URL yet) and the host that serves its files.
func newParts(t *testing.T) (*catalog.AppleEngine, *parthost) {
	t.Helper()
	var tgz bytes.Buffer
	zw := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(zw)
	const py = "\xcf\xfa\xed\xfe"
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "python/bin/python3.13", Mode: 0o755, Size: int64(len(py)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(py))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	zipOf := func(name, content string) []byte {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		return b.Bytes()
	}
	h := &parthost{body: map[string][]byte{}}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		b, ok := h.body[r.URL.Path]
		h.hits = append(h.hits, r.URL.Path)
		h.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "f", time.Time{}, bytes.NewReader(b))
	}))
	t.Cleanup(h.Close)

	apple := &catalog.AppleEngine{
		Label: "Apple Silicon", Commit: strings.Repeat("a", 40), Python: "python/bin/python3.13", Main: "ComfyUI/main.py", MinMacOS: 14,
		SitePackages: "python/lib/python3.13/site-packages", Env: []string{"PYTORCH_ENABLE_MPS_FALLBACK=1"},
	}
	for _, p := range []struct {
		part parts.Part
		body []byte
	}{
		{parts.Part{Name: "python", Kind: parts.KindTarGz, ExtractedSize: int64(len(py))}, tgz.Bytes()},
		{parts.Part{Name: "comfyui", Kind: parts.KindZip, ExtractedSize: 7, Dest: "ComfyUI", Strip: "ComfyUI-abc"}, zipOf("ComfyUI-abc/main.py", "# comfy")},
		{parts.Part{Name: "torch", Kind: parts.KindWheel, ExtractedSize: 7}, zipOf("torch/__init__.py", "# torch")},
	} {
		sum := sha256.Sum256(p.body)
		p.part.SHA256, p.part.Size = hex.EncodeToString(sum[:]), int64(len(p.body))
		p.part.URL = h.URL + "/" + p.part.Name
		h.body["/"+p.part.Name] = p.body
		apple.Parts = append(apple.Parts, p.part)
		apple.Size += p.part.Size
		apple.ExtractedSize += p.part.ExtractedSize
	}
	return apple, h
}

// mac makes the Service one of a Mac (platform "darwin", the architecture and macOS version given) whose catalog has the macOS build, with
// the data directory below a temporary directory: the engine is not inside the app. The GPU detection answers an Apple silicon chip.
func mac(t *testing.T, goos, goarch string, macOS int) (*mmtest.Rig, *parthost) {
	t.Helper()
	apple, host := newParts(t)
	return mmtest.New(t, func(d *modelmanager.Deps, r *mmtest.Rig) {
		d.Catalog.Engine.Apple = apple
		d.Platform, d.Arch = goos, goarch
		d.MacOSMajor = func(context.Context) (int, error) { return macOS, nil }
		d.Layout = store.For("darwin", exe, filepath.Join(t.TempDir(), "MAM"))
		r.Layout = d.Layout
		r.GPUReport = gpu.Report{OS: "darwin", Recommendation: gpu.Recommendation{Variant: catalog.VariantApple, Confidence: "high", Reason: "Apple M2"}}
	}), host
}

func TestAMacOffersTheOneAppleBuildAndInstallsItOutsideTheApp(t *testing.T) {
	r, host := mac(t, "darwin", "arm64", 15)
	v := r.Svc.Engine()
	require.True(t, v.Supported)
	require.Len(t, v.Variants, 1, "the portable builds of Windows are not offered on a Mac")
	assert.Equal(t, catalog.VariantApple, v.Variants[0].Variant)
	assert.Equal(t, "Apple Silicon", v.Variants[0].Label)
	assert.Equal(t, r.Svc.Deps.Catalog.Engine.Apple.Size, v.Variants[0].Size)
	assert.Positive(t, v.Variants[0].NeedBytes)
	assert.NotContains(t, v.Dir, ".app", "the engine and the models are not inside MAM.app, which the update replaces")
	assert.Equal(t, r.Layout.Root, v.Dir)
	assert.Equal(t, "comfyui", filepath.Base(v.Dir))
	assert.Equal(t, "MAM", filepath.Base(filepath.Dir(v.Dir)), "in the data directory")

	var phases []string
	cancel := r.Svc.Watch(func(e modelmanager.Event) {
		if e.Job != nil && (len(phases) == 0 || phases[len(phases)-1] != e.Job.Update.Phase) {
			phases = append(phases, e.Job.Update.Phase)
		}
	})
	defer cancel()
	ref, err := r.Svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{}) // the build of the detected GPU
	require.NoError(t, err)
	st := r.Wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, []string{"download", "extract", "register", "done"}, phases)
	assert.ElementsMatch(t, []string{"/python", "/comfyui", "/torch"}, host.hits, "each part once")

	v = r.Svc.Engine()
	assert.True(t, v.Installed)
	assert.Equal(t, "apple", v.Variant)
	assert.False(t, v.CPU)
	assert.FileExists(t, filepath.Join(r.Layout.EngineDir(), "python", "bin", "python3.13"))
	assert.FileExists(t, filepath.Join(r.Layout.EngineDir(), "ComfyUI", "main.py"))
	assert.FileExists(t, filepath.Join(r.Layout.EngineDir(), "python", "lib", "python3.13", "site-packages", "torch", "__init__.py"))

	// The managed endpoint is registered, and the engine starts and stops on this OS as on Windows (the stand-in process answers).
	status, err := r.Svc.Status(context.Background())
	require.NoError(t, err)
	require.Len(t, status.Endpoints, 1)
	assert.True(t, status.Endpoints[0].Bundled)
	ps, err := r.Svc.StartEngine(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "running", string(ps.State))
	require.NoError(t, r.Svc.StopEngine())
}

func TestAMacRefusesAWindowsBuildAndWindowsRefusesTheMacBuild(t *testing.T) {
	r, host := mac(t, "darwin", "arm64", 14)
	_, err := r.Svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
	require.ErrorIs(t, err, engine.ErrVariant)
	assert.Empty(t, host.hits, "nothing was downloaded")

	win, _ := mac(t, "windows", "amd64", 0)
	_, err = win.Svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{Variant: catalog.VariantApple})
	require.ErrorIs(t, err, engine.ErrVariant)
	require.Len(t, win.Svc.Engine().Variants, 1, "the Windows builds of the catalog")
	assert.Equal(t, catalog.VariantNVIDIA, win.Svc.Engine().Variants[0].Variant)
}

func TestAMacRunsOnTheCPUOnlyWhenAskedTo(t *testing.T) {
	r, _ := mac(t, "darwin", "arm64", 14)
	r.GPUReport = gpu.Report{} // no GPU was found: nothing is chosen without an explicit CPU run
	_, err := r.Svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{})
	require.ErrorIs(t, err, modelmanager.ErrNoGPU)

	ref, err := r.Svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{CPU: true})
	require.NoError(t, err)
	st := r.Wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	v := r.Svc.Engine()
	assert.Equal(t, "apple", v.Variant, "the one build of a Mac, started with --cpu")
	assert.True(t, v.CPU)
}

// An Intel Mac and a macOS the pinned wheels do not run on use an external ComfyUI, as every other OS does.
func TestAMacThatCannotRunTheBuildGetsTheExternalComfyUIAnswer(t *testing.T) {
	for name, tc := range map[string]struct {
		goarch string
		macOS  int
	}{"an Intel Mac": {"amd64", 14}, "macOS 13": {"arm64", 13}} {
		t.Run(name, func(t *testing.T) {
			r, host := mac(t, "darwin", tc.goarch, tc.macOS)
			assert.False(t, r.Svc.Engine().Supported)
			_, err := r.Svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{CPU: true})
			require.ErrorIs(t, err, modelmanager.ErrUnsupported)
			_, err = r.Svc.StartEngine(context.Background())
			require.ErrorIs(t, err, modelmanager.ErrUnsupported)
			require.ErrorIs(t, r.Svc.Prepare(context.Background(), modelmanager.BundledEndpoint), modelmanager.ErrUnsupported)
			assert.Empty(t, host.hits)
		})
	}
}
