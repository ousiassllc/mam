package engine_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/extract"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

const sitePackages = "python/lib/python3.13/site-packages"

// appleRig serves the three kinds of parts of the macOS build (a Python archive, the ComfyUI source, a wheel) from a fake host, and installs
// them with an Installer that has the catalog entry of the build.
type appleRig struct {
	inst *engine.Installer
	srv  *httptest.Server

	mu   sync.Mutex
	body map[string][]byte // URL path -> bytes served
	hits map[string]int
}

func gz(t *testing.T, entries map[string]string, modes map[string]int64, links map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		mode := modes[n]
		if mode == 0 {
			mode = 0o644
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: n, Mode: mode, Size: int64(len(entries[n])), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(entries[n]))
		require.NoError(t, err)
	}
	for n, target := range links {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeSymlink, Linkname: target}))
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		out, err := w.Create(n)
		require.NoError(t, err)
		_, err = out.Write([]byte(entries[n]))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func total(entries map[string]string) (n int64) {
	for _, v := range entries {
		n += int64(len(v))
	}
	return n
}

func newAppleRig(t *testing.T) *appleRig {
	t.Helper()
	r := &appleRig{body: map[string][]byte{}, hits: map[string]int{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		b, ok := r.body[req.URL.Path]
		r.hits[req.URL.Path]++
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		http.ServeContent(w, req, "x", zeroTime, bytes.NewReader(b)) // Range support
	}))
	t.Cleanup(r.srv.Close)

	pyFiles := map[string]string{"python/bin/python3.13": "\xcf\xfa\xed\xfe", "python/lib/python3.13/os.py": "# stdlib"}
	py := gz(t, pyFiles, map[string]int64{"python/bin/python3.13": 0o775}, map[string]string{"python/bin/python": "python3.13"})
	srcFiles := map[string]string{"ComfyUI-abc/": "", "ComfyUI-abc/main.py": "# comfy", "ComfyUI-abc/comfy/model_management.py": "# mm"}
	delete(srcFiles, "ComfyUI-abc/")
	src := zipOf(t, srcFiles)
	whlFiles := map[string]string{"torch/__init__.py": "# torch", "torch-2.15.0.dist-info/METADATA": "Name: torch", "torch-2.15.0.data/scripts/torchrun": "#!x"}
	whl := zipOf(t, whlFiles)
	r.body["/python.tar.gz"], r.body["/comfy.zip"], r.body["/torch.whl"] = py, src, whl

	part := func(name, kind, path string, b []byte, ext int64) parts.Part {
		sum := sha256.Sum256(b)
		return parts.Part{Name: name, Kind: kind, URL: r.srv.URL + path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b)), ExtractedSize: ext}
	}
	pp := part("cpython", parts.KindTarGz, "/python.tar.gz", py, total(pyFiles))
	sp := part("comfyui", parts.KindZip, "/comfy.zip", src, total(srcFiles))
	sp.Dest, sp.Strip = "ComfyUI", "ComfyUI-abc"
	wp := part("torch", parts.KindWheel, "/torch.whl", whl, total(whlFiles))
	ps := []parts.Part{pp, sp, wp}

	l := store.Layout{Root: filepath.Join(t.TempDir(), "mam-data", "comfyui")}
	st, err := store.Open(l)
	require.NoError(t, err)
	apple := &catalog.AppleEngine{
		Label: "Apple", Commit: strings.Repeat("a", 40), Python: "python/bin/python3.13", Main: "ComfyUI/main.py", SitePackages: sitePackages,
		MinMacOS: 14, Env: []string{"PYTORCH_ENABLE_MPS_FALLBACK=1"}, Parts: ps,
	}
	for _, p := range ps {
		apple.Size += p.Size
		apple.ExtractedSize += p.ExtractedSize
	}
	r.inst = &engine.Installer{
		Layout: l, Store: st, Extract: extract.SevenZip{},
		Engine: catalog.Engine{Version: "v0.42.1", Python: pythonExe, Main: "ComfyUI/main.py", Apple: apple},
		Fetch: fetch.Options{
			CheckURL: func(*url.URL) error { return nil },
			Client:   fetch.WithRedirectRules(&http.Client{}, func(*url.URL) error { return nil }, ""),
		},
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
	}
	return r
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
