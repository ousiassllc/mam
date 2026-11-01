// Package lrtest is a localruntime.Service over fakes, for the tests of the layers above it (the screen's handlers, the MCP tools): a fake
// Hugging Face that serves a package and two weights, a fake runtime process, and a temporary directory next to the "exe". The package
// is the synthetic 7z of the extract tests; the process that is started is the test binary itself, so a package that starts it calls
// ServeIfAsked from its TestMain. The catalog is the real GPT-SoVITS runtime (its support rules and texts) with the test's package and
// weights; the weights include one whose commercial use is "unclear", so a screen test sees the prominent note without any real data of
// that kind. Not for production code.
package lrtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// The weights of the test catalog and where they go below the environment directory.
var (
	WeightBytes = []byte(strings.Repeat("w", 5000))
	ExtraBytes  = []byte(strings.Repeat("e", 700))
)

// The paths below the environment directory and on the fake host.
const (
	WeightDest = "models/model.bin"
	ExtraDest  = "models/extra.bin"
	ID         = "gpt-sovits"

	weightPath  = "/w/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/model.bin"
	extraPath   = "/w/org/repo/resolve/0123456789abcdef0123456789abcdef01234567/extra.bin"
	archiveRoot = "ComfyUI_windows_portable"
	pythonRel   = "python_embeded/python.exe" //nolint:misspell // the directory name of the upstream package
	entryRel    = "ComfyUI/main.py"
)

// ServeIfAsked turns the test binary into the fake runtime server when MAM_FAKE_RUNTIME is set (it answers /openapi.json on FAKE_PORT;
// "exit" makes it write to stderr and exit at once) and reports true; the TestMain of a package that starts a runtime returns then.
//
//	func TestMain(m *testing.M) {
//		if lrtest.ServeIfAsked() { return }
//		...
//	}
func ServeIfAsked() bool {
	switch os.Getenv("MAM_FAKE_RUNTIME") {
	case "":
		return false
	case "exit":
		_, _ = os.Stderr.WriteString("ImportError: DLL load failed while importing torch\n")
		os.Exit(3)
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_PORT"))
	if err != nil {
		os.Exit(4)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	_ = (&http.Server{Handler: mux, ReadHeaderTimeout: time.Minute}).Serve(l)
	return true
}

// Host is the fake Hugging Face: the package and the weights, with Range. A path in Gone answers 404.
type Host struct {
	*httptest.Server
	body []byte
	mu   sync.Mutex
	// Files are the files by path; Gone are the paths that answer 404.
	Files map[string][]byte
	Gone  map[string]bool
	reqs  []string
	hold  map[string]chan struct{}
	// reached is told when a held request arrived.
	reached chan struct{}
}

// Hold makes the requests for path wait until release is called (or the client gives up); Reached then says one arrived. A test that
// stops a running install uses it to have the job in the middle of a download.
func (h *Host) Hold(path string) (release func()) {
	ch := make(chan struct{})
	h.mu.Lock()
	h.hold[path] = ch
	h.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// Reached waits until a held request arrived.
func (h *Host) Reached(t *testing.T) {
	t.Helper()
	select {
	case <-h.reached:
	case <-time.After(20 * time.Second):
		t.Fatal("the request never reached the host")
	}
}

// WeightURLPath and ExtraURLPath are the paths of the two weights on the host.
const (
	WeightURLPath = weightPath
	ExtraURLPath  = extraPath
)

// Requests are the paths asked for (with the Range header), in order.
func (h *Host) Requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.reqs...)
}

// Rig is the Service and what it runs on.
type Rig struct {
	T      *testing.T
	Svc    *localruntime.Service
	Host   *Host
	Root   string
	Layout store.RuntimeLayout
	// GPU is what the detection answers; Free the disk space the Service sees.
	GPU  gpu.Report
	Free uint64
	// Others are the jobs that were not a runtime's.
	Others chan domain.Job
}

// Option changes the Deps (and the Rig) before the Service is made.
type Option func(*Rig, *localruntime.Deps)

// Platform makes the Service one of another OS and architecture (the default is Windows x64 with an RTX 4070).
func Platform(os, arch string) Option {
	return func(_ *Rig, d *localruntime.Deps) { d.Platform = rtm.Platform{OS: os, Arch: arch} }
}

// GPUs sets the adapters the detection lists.
func GPUs(a ...gpu.Adapter) Option {
	return func(r *Rig, _ *localruntime.Deps) { r.GPU = gpu.Report{Adapters: a} }
}

// NVIDIA is an NVIDIA adapter with vramGiB GiB.
func NVIDIA(name string, vramGiB uint64, cc float64) gpu.Adapter {
	return gpu.Adapter{Name: name, VendorID: gpu.VendorNVIDIA, VRAMBytes: vramGiB << 30, ComputeCap: cc}
}

// New builds a Rig on the queue and job table of the caller (which starts the queue). The Service registers itself as the handler of
// model_install, handing the jobs that are not a runtime's to next (nil: the Others channel).
func New(t *testing.T, queue localruntime.Queue, table *repository.JobRepository, next jobqueue.Handler, opts ...Option) *Rig {
	t.Helper()
	r := &Rig{T: t, Free: 1 << 40, Others: make(chan domain.Job, 8)}
	r.GPU = gpu.Report{Adapters: []gpu.Adapter{NVIDIA("NVIDIA GeForce RTX 4070", 12, 8.9)}}
	r.Root = filepath.Join(t.TempDir(), "install", "localai")
	r.Layout = store.ForRuntime(r.Root, ID)
	r.Host = newHost(t)
	anyURL := func(*url.URL) error { return nil }
	d := localruntime.Deps{
		Root: r.Root, Catalog: testCatalog(r.Host), Queue: queue, JobTable: table,
		Platform:  rtm.Platform{OS: "windows", Arch: "amd64"},
		GPU:       func(context.Context) gpu.Report { return r.GPU },
		FreeBytes: func(string) (uint64, error) { return r.Free, nil },
		Fetch:     fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "")},
		Command: func(_ string, port int) (proc.Spec, error) {
			return proc.Spec{Command: os.Args[0], LogPath: r.Layout.LogPath(), Env: []string{"MAM_FAKE_RUNTIME=serve", "FAKE_PORT=" + strconv.Itoa(port)}}, nil
		},
		PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second,
	}
	for _, o := range opts {
		o(r, &d)
	}
	var err error
	r.Svc, err = localruntime.New(d)
	require.NoError(t, err)
	if next == nil {
		next = func(_ context.Context, job domain.Job) error { r.Others <- job; return nil }
	}
	r.Svc.Register(next)
	t.Cleanup(r.Svc.Close)
	return r
}

// Wait returns the status of the job once it ended (polling with an upper limit). The queue must be started.
func (r *Rig) Wait(jobID string) localruntime.JobStatus {
	r.T.Helper()
	var st localruntime.JobStatus
	require.Eventually(r.T, func() bool {
		var err error
		st, err = r.Svc.JobStatus(context.Background(), jobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}

// WeightPath is where the installed weight lies (for a test that checks the disk).
func (r *Rig) WeightPath(dest string) string {
	return filepath.Join(r.Layout.EnvDir(), filepath.FromSlash(dest))
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func newHost(t *testing.T) *Host {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	body, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "..", "..", "modelmgr", "extract", "testdata", "bcj2.7z"))
	require.NoError(t, err)
	h := &Host{body: body, Files: map[string][]byte{weightPath: WeightBytes, extraPath: ExtraBytes, "/pkg.7z": body}, Gone: map[string]bool{}, hold: map[string]chan struct{}{}, reached: make(chan struct{}, 8)}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		data, ok := h.Files[r.URL.Path]
		gone, hold := h.Gone[r.URL.Path], h.hold[r.URL.Path]
		h.reqs = append(h.reqs, r.URL.Path+" "+r.Header.Get("Range"))
		h.mu.Unlock()
		if hold != nil {
			h.reached <- struct{}{}
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		if !ok || gone {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "f", time.Time{}, strings.NewReader(string(data)))
	}))
	t.Cleanup(h.Close)
	return h
}

// testCatalog is the real runtime of GPT-SoVITS (its support rules and texts) over the test's package, launch and weights.
func testCatalog(h *Host) *catalog.Catalog {
	variant := func(minCC float64) catalog.RuntimeVariant {
		return catalog.RuntimeVariant{
			Asset:         catalog.Asset{Label: "test", URL: h.URL + "/pkg.7z", SHA256: sum(h.body), Size: int64(len(h.body)), ExtractedSize: 1 << 20},
			MinComputeCap: minCC,
		}
	}
	real, _ := catalog.Default().Runtime(ID)
	rt := real
	rt.ArchiveRoot, rt.Version, rt.ExtraFiles = archiveRoot, "1", nil // the test's package has no launcher of mam's to write
	rt.Launch = catalog.RuntimeLaunch{
		Python: pythonRel, Entry: entryRel, Args: []string{entryRel, "-a", "127.0.0.1", "-p", "{port}"}, ReadyPath: "/openapi.json",
		Env: []string{"HF_HUB_OFFLINE=1"}, StartTimeoutSec: 20,
	}
	rt.Variants = map[catalog.Variant]catalog.RuntimeVariant{catalog.VariantNVIDIA: variant(0), catalog.VariantNVIDIA50: variant(12)}
	return &catalog.Catalog{Runtimes: []catalog.Runtime{rt}, ResourceSets: []catalog.ResourceSet{{
		Runtime: ID,
		Licenses: []catalog.ResourceLicense{
			{ID: "mit", Name: "MIT License", URL: "https://example.com/mit", Summary: "商用利用を認めるライセンス", CommercialUse: catalog.CommercialYes},
			{ID: "odd", Name: "不明なライセンス", URL: "https://example.com/odd", Summary: "配布元に表示がない", CommercialUse: catalog.CommercialUnclear, Notice: "商用で使う前に配布元に確認してください"},
		},
		Assets: []catalog.Resource{
			{
				Name: "model", Description: "本体の重み", Dest: WeightDest, URL: h.URL + weightPath, SHA256: sum(WeightBytes), Size: int64(len(WeightBytes)), Required: true,
				Source: catalog.ResourceSource{Name: "org/repo", URL: "https://huggingface.co/org/repo"}, License: "mit",
			},
			{
				Name: "extra", Description: "補助の依存ファイル", Dest: ExtraDest, URL: h.URL + extraPath, SHA256: sum(ExtraBytes), Size: int64(len(ExtraBytes)), Required: false,
				Source: catalog.ResourceSource{Name: "x", URL: "https://example.com/x", Note: "原本は x。2026-11-01 に SHA-256 の一致を確認した"}, License: "odd",
			},
		},
	}}}
}

// Queue is the part of the job queue the Service uses (a *jobqueue.Queue).
var _ localruntime.Queue = (*jobqueue.Queue)(nil)
