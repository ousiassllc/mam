package localruntime_test

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
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// The synthetic package is the 7z of the extract tests (LZMA2 + BCJ2): ComfyUI_windows_portable/{<python dir>/python.exe,
// ComfyUI/main.py}. It stands in for the vendor's package; the process that is started is this test binary, not that python.exe.
const (
	archiveFile = "../../modelmgr/extract/testdata/bcj2.7z"
	archiveRoot = "ComfyUI_windows_portable"
	pythonRel   = "python_embeded/python.exe" //nolint:misspell // the directory name of the upstream package
	entryRel    = "ComfyUI/main.py"
	gib         = 1 << 30
)

// TestMain turns the test binary into the fake runtime server when MAM_FAKE_RUNTIME is set: it answers /openapi.json on FAKE_PORT
// ("exit" makes it write to its log and exit at once).
func TestMain(m *testing.M) {
	switch os.Getenv("MAM_FAKE_RUNTIME") {
	case "":
		os.Exit(m.Run())
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
}

// host serves the package and can be told to cut the first answer in half or to hold it until released.
type host struct {
	*httptest.Server
	body []byte
	mu   sync.Mutex
	// ranges are the Range headers of the requests; cut: the next answer drops in the middle; hold: the next answer waits for release.
	ranges []string
	// files are more files the host serves by path (the weights of a test catalog); gone are the paths that answer 404.
	files   map[string][]byte
	cut     bool
	hold    chan struct{}
	reached chan struct{}
}

func newHost(t *testing.T) *host {
	t.Helper()
	body, err := os.ReadFile(archiveFile)
	require.NoError(t, err)
	h := &host{body: body, reached: make(chan struct{}, 8), files: map[string][]byte{}}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		extra, isExtra := h.files[r.URL.Path]
		if isExtra {
			h.ranges = append(h.ranges, r.URL.Path+" "+r.Header.Get("Range"))
		}
		h.mu.Unlock()
		if isExtra {
			http.ServeContent(w, r, "w", time.Time{}, strings.NewReader(string(extra)))
			return
		}
		if r.URL.Path != "/pkg.7z" {
			http.NotFound(w, r)
			return
		}
		h.mu.Lock()
		h.ranges = append(h.ranges, r.Header.Get("Range"))
		cut, hold := h.cut, h.hold
		h.cut = false
		h.mu.Unlock()
		if cut {
			w.Header().Set("Content-Length", strconv.Itoa(len(h.body)))
			_, _ = w.Write(h.body[:len(h.body)/2])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		if hold != nil {
			h.reached <- struct{}{}
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		http.ServeContent(w, r, "p.7z", time.Time{}, strings.NewReader(string(h.body)))
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *host) Ranges() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.ranges...)
}

// Hold makes the next answer wait; the returned function lets it through.
func (h *host) Hold() (release func()) {
	ch := make(chan struct{})
	h.mu.Lock()
	h.hold = ch
	h.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func (h *host) Reached(t *testing.T) {
	t.Helper()
	select {
	case <-h.reached:
	case <-time.After(20 * time.Second):
		t.Fatal("the request never reached the host")
	}
}

func testCatalog(h *host) *catalog.Catalog {
	sum := sha256.Sum256(h.body)
	variant := func(minCC float64) catalog.RuntimeVariant {
		return catalog.RuntimeVariant{
			Asset:         catalog.Asset{Label: "test", URL: h.URL + "/pkg.7z", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(h.body)), ExtractedSize: 1 << 20},
			MinComputeCap: minCC,
		}
	}
	real, _ := catalog.Default().Runtime("gpt-sovits")
	rt := real                                                        // the real support rules and texts; the package, the files and the launch are the test's
	rt.ArchiveRoot, rt.Version, rt.ExtraFiles = archiveRoot, "1", nil // the test's package has no launcher of mam's to write
	rt.Launch = catalog.RuntimeLaunch{
		Python: pythonRel, Entry: entryRel, Args: []string{entryRel, "-a", "127.0.0.1", "-p", "{port}"}, ReadyPath: "/openapi.json",
		Env: []string{"HF_HUB_OFFLINE=1"}, StartTimeoutSec: 20,
	}
	rt.Variants = map[catalog.Variant]catalog.RuntimeVariant{catalog.VariantNVIDIA: variant(0), catalog.VariantNVIDIA50: variant(12)}
	return &catalog.Catalog{Runtimes: []catalog.Runtime{rt}}
}

// rig is the Service over the fakes.
type rig struct {
	t      *testing.T
	svc    *localruntime.Service
	host   *host
	queue  *jobqueue.Queue
	table  *repository.JobRepository
	root   string
	layout store.RuntimeLayout
	// others are the jobs that were not a runtime's: the handler the model manager would have.
	others chan domain.Job
	// gpu is what the detection answers; free the disk space; mode what the fake process does (MAM_FAKE_RUNTIME).
	gpu     gpu.Report
	free    uint64
	mode    string
	started bool
}

type option func(*rig, *localruntime.Deps)

func platform(os, arch string) option {
	return func(_ *rig, d *localruntime.Deps) { d.Platform = rtm.Platform{OS: os, Arch: arch} }
}

func withGPU(adapters ...gpu.Adapter) option {
	return func(r *rig, _ *localruntime.Deps) { r.gpu = gpu.Report{Adapters: adapters} }
}

func rtx(name string, vramGiB uint64, cc float64) gpu.Adapter {
	return gpu.Adapter{Name: name, VendorID: gpu.VendorNVIDIA, VRAMBytes: vramGiB * gib, ComputeCap: cc}
}

// newRig is a Windows x64 machine with an RTX 4070 unless an option says another.
func newRig(t *testing.T, opts ...option) *rig {
	t.Helper()
	r := &rig{t: t, host: newHost(t), others: make(chan domain.Job, 8), mode: "serve", free: 1 << 40}
	r.gpu = gpu.Report{Adapters: []gpu.Adapter{rtx("NVIDIA GeForce RTX 4070", 12, 8.9)}}
	r.root = filepath.Join(t.TempDir(), "install", "localai")
	r.layout = store.ForRuntime(r.root, "gpt-sovits")
	db := testdb.Open(t)
	r.table = repository.NewJobRepository(db)
	r.queue = jobqueue.New(r.table, jobqueue.Options{Workers: 2})
	anyURL := func(*url.URL) error { return nil }
	d := localruntime.Deps{
		Root: r.root, Catalog: testCatalog(r.host), Queue: r.queue, JobTable: r.table,
		Platform:  rtm.Platform{OS: "windows", Arch: "amd64"},
		GPU:       func(context.Context) gpu.Report { return r.gpu },
		FreeBytes: func(string) (uint64, error) { return r.free, nil },
		Fetch:     fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "")},
		Command: func(_ string, port int) (proc.Spec, error) {
			return proc.Spec{
				Command: os.Args[0], LogPath: r.layout.LogPath(),
				Env: []string{"MAM_FAKE_RUNTIME=" + r.mode, "FAKE_PORT=" + strconv.Itoa(port)},
			}, nil
		},
		PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second,
	}
	for _, o := range opts {
		o(r, &d)
	}
	var err error
	r.svc, err = localruntime.New(d)
	require.NoError(t, err)
	r.svc.Register(func(_ context.Context, job domain.Job) error { r.others <- job; return nil })
	t.Cleanup(func() { r.queue.Stop(); r.svc.Close() })
	return r
}

func (r *rig) start() {
	r.t.Helper()
	if !r.started {
		r.started = true
		r.queue.Start(context.Background())
	}
}

// wait returns the status of the job once it ended (polling with an upper limit).
func (r *rig) wait(jobID string) localruntime.JobStatus {
	r.t.Helper()
	r.start()
	var st localruntime.JobStatus
	require.Eventually(r.t, func() bool {
		var err error
		st, err = r.svc.JobStatus(context.Background(), jobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}

// install installs the runtime and waits for it.
func (r *rig) install() {
	r.t.Helper()
	ref, err := r.svc.Install(context.Background(), "gpt-sovits")
	require.NoError(r.t, err)
	st := r.wait(ref.JobID)
	require.Equal(r.t, domain.JobSucceeded, st.State, st.Error)
}
