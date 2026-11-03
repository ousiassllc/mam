package llamaservertest_test

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

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

func TestMain(m *testing.M) {
	if maybeServeFakeLlama() {
		return
	}
	os.Exit(m.Run())
}

func ctx() context.Context { return context.Background() }

func sum256(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// zipOf is a zip of the named files (stored: the test binary is large), each with the mode of an executable.
func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0o755)
		f, err := w.CreateHeader(h)
		require.NoError(t, err)
		_, err = f.Write(body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// The package the fake host serves, built once: the test binary as llama-server.exe, and the DLLs.
var (
	buildOnce      sync.Once
	llamaZip       []byte
	cudartZip      []byte
	llamaExtracted int64
)

func packageBytes(t *testing.T) (llama, cudart []byte) {
	t.Helper()
	buildOnce.Do(func() {
		self, err := os.ReadFile(os.Args[0])
		require.NoError(t, err)
		llamaZip = zipOf(t, map[string][]byte{programName: self, "ggml-cuda.dll": []byte("cuda"), "llama.dll": []byte("llama")})
		cudartZip = zipOf(t, map[string][]byte{"cudart64_12.dll": []byte("cudart"), "cublas64_12.dll": []byte("cublas")})
		llamaExtracted = int64(len(self)) + 4 + 5
	})
	return llamaZip, cudartZip
}

// Paths on the fake host.
const (
	llamaPath  = "/llama/cuda-12.4.zip"
	llama13    = "/llama/cuda-13.4.zip"
	cudartPath = "/llama/cudart-12.4.zip"
	cudart13   = "/llama/cudart-13.4.zip"
)

// host is the fake GitHub and Hugging Face: the package and the weights (with Range), a log of what was asked, and a way to make a path answer with other bytes.
type host struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	asked []string
}

func (h *host) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.asked...)
}

func (h *host) serve(path string, body []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.files[path] = body
}

// weightBody is the fake file of a model: distinct per model, so that the wrong one cannot pass for another.
func weightBody(choice string) []byte {
	return append([]byte("GGUF"), bytes.Repeat([]byte(choice+";"), 300)...)
}

// fakeCatalog is the real llama.cpp runtime (its launch, token, support rules and texts, the real models with their licenses) over a package of fake
// zips and fake weights that the host serves.
func fakeCatalog(t *testing.T, h *host) *catalog.Catalog {
	t.Helper()
	llama, cudart := packageBytes(t)
	for path, body := range map[string][]byte{llamaPath: llama, llama13: llama, cudartPath: cudart, cudart13: cudart} {
		h.files[path] = body
	}
	c := catalog.Default()
	real, ok := c.Runtime("llama-cpp")
	require.True(t, ok)
	variant := func(name, llamaURL, cudartURL string) catalog.RuntimeVariant {
		ps := []parts.Part{
			{Name: "llama-" + name, Kind: parts.KindZip, URL: h.URL + llamaURL, SHA256: sum256(llama), Size: int64(len(llama)), ExtractedSize: llamaExtracted},
			{Name: "cudart-" + name, Kind: parts.KindZip, URL: h.URL + cudartURL, SHA256: sum256(cudart), Size: int64(len(cudart)), ExtractedSize: 6 + 6},
		}
		return catalog.RuntimeVariant{Asset: catalog.Asset{Label: name, Size: ps[0].Size + ps[1].Size, ExtractedSize: ps[0].ExtractedSize + ps[1].ExtractedSize}, Parts: ps}
	}
	rt := real
	rt.Variants = map[catalog.Variant]catalog.RuntimeVariant{
		catalog.VariantNVIDIA:   variant("nvidia", llamaPath, cudartPath),
		catalog.VariantNVIDIA50: variant("nvidia50", llama13, cudart13),
	}
	rt.Variants[catalog.VariantNVIDIA50] = withMinCC(rt.Variants[catalog.VariantNVIDIA50], real.Variants[catalog.VariantNVIDIA50].MinComputeCap)
	rt.Launch.StartTimeoutSec = 20
	out := *c
	out.Runtimes = []catalog.Runtime{rt}
	set, ok := c.ResourceSet("llama-cpp")
	require.True(t, ok)
	set.Assets = append([]catalog.Resource(nil), set.Assets...)
	for i := range set.Assets {
		a := &set.Assets[i]
		body := weightBody(a.Choice)
		path := "/w/" + a.Choice + ".gguf"
		h.files[path] = body
		a.URL, a.SHA256, a.Size = h.URL+path, sum256(body), int64(len(body))
	}
	out.ResourceSets = []catalog.ResourceSet{set}
	return &out
}

func withMinCC(v catalog.RuntimeVariant, cc float64) catalog.RuntimeVariant {
	v.MinComputeCap = cc
	return v
}

// rig is the Service over the fakes: a Windows x64 machine with an RTX 4070 Ti SUPER (16 GiB) unless an option says another.
type rig struct {
	t       *testing.T
	svc     *localruntime.Service
	host    *host
	root    string
	layout  store.RuntimeLayout
	queue   *jobqueue.Queue
	gpu     gpu.Report
	started bool
}

type option func(*rig, *localruntime.Deps)

func platform(os, arch string) option {
	return func(_ *rig, d *localruntime.Deps) { d.Platform = rtm.Platform{OS: os, Arch: arch} }
}

func withGPU(a gpu.Adapter) option {
	return func(r *rig, _ *localruntime.Deps) { r.gpu = gpu.Report{Adapters: []gpu.Adapter{a}} }
}

func rtx(name string, vramGiB uint64, cc float64) gpu.Adapter {
	return gpu.Adapter{Name: name, VendorID: gpu.VendorNVIDIA, VRAMBytes: vramGiB << 30, ComputeCap: cc}
}

func newRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	r := &rig{t: t, gpu: gpu.Report{Adapters: []gpu.Adapter{rtx("NVIDIA GeForce RTX 4070 Ti SUPER", 16, 8.9)}}}
	r.host = &host{files: map[string][]byte{}}
	r.host.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.host.mu.Lock()
		body, ok := r.host.files[req.URL.Path]
		r.host.asked = append(r.host.asked, req.URL.Path)
		r.host.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		http.ServeContent(w, req, "f", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(r.host.Close)
	r.root = filepath.Join(t.TempDir(), "install", "localai")
	r.layout = store.ForRuntime(r.root, "llama-cpp")
	table := repository.NewJobRepository(testdb.Open(t))
	r.queue = jobqueue.New(table, jobqueue.Options{Workers: 2})
	anyURL := func(*url.URL) error { return nil }
	d := localruntime.Deps{
		Root: r.root, Catalog: fakeCatalog(t, r.host), Queue: r.queue, JobTable: table,
		Platform:  rtm.Platform{OS: "windows", Arch: "amd64"},
		GPU:       func(context.Context) gpu.Report { return r.gpu },
		FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
		Fetch:     fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "")},
		PollEvery: 20 * time.Millisecond, StartTimeout: 30 * time.Second,
	}
	for _, o := range opts {
		o(r, &d)
	}
	var err error
	r.svc, err = localruntime.New(d)
	require.NoError(t, err)
	r.svc.Register(func(context.Context, domain.Job) error { return nil })
	t.Cleanup(func() { r.queue.Stop(); r.svc.Close() })
	return r
}

func (r *rig) run(ref localruntime.JobRef) localruntime.JobStatus {
	r.t.Helper()
	if !r.started {
		r.started = true
		r.queue.Start(ctx())
	}
	var st localruntime.JobStatus
	var err error
	require.Eventually(r.t, func() bool {
		st, err = r.svc.JobStatus(ctx(), ref.JobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 60*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}

// install installs the runtime and the weights of the model in force, and fails the test when the job fails.
func (r *rig) install() {
	r.t.Helper()
	ref, err := r.svc.Install(ctx(), "llama-cpp")
	require.NoError(r.t, err)
	st := r.run(ref)
	require.Equal(r.t, domain.JobSucceeded, st.State, st.Error)
}

func (r *rig) installWeights() localruntime.JobStatus {
	r.t.Helper()
	ref, err := r.svc.InstallWeights(ctx(), "llama-cpp")
	require.NoError(r.t, err)
	return r.run(ref)
}

// argv is the arguments the fake llama-server was started with (it writes them into its working directory, the environment directory).
func (r *rig) argv() []string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.layout.EnvDir(), "argv.txt"))
	require.NoError(r.t, err)
	return strings.Split(string(b), "\n")
}
