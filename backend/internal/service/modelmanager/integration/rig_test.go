// Package integration tests the model manager service end to end (a fake GitHub / Hugging Face, a fake ComfyUI
// engine, ComfyUI fakes). It has its own directory so that the service directory stays under the line limit.
package integration_test

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
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/detect"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

const archivePath = "../../../modelmgr/extract/testdata/bcj2.7z"

// TestMain turns the test binary into the fake ComfyUI engine when MAM_FAKE_COMFY is set: it listens on
// 127.0.0.1:$FAKE_PORT and answers /system_stats.
// pythonExe is the path of the interpreter in the upstream package; the directory name is spelled that way upstream.
const pythonExe = "python_embeded/python.exe" //nolint:misspell // upstream spelling

func TestMain(m *testing.M) {
	if os.Getenv("MAM_FAKE_COMFY") != "" {
		l, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("FAKE_PORT"))
		if err != nil {
			os.Exit(4)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/system_stats", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"fake"}}`))
		})
		_ = http.Serve(l, mux)
		return
	}
	os.Exit(m.Run())
}

func fakeEngine(port int) (proc.Spec, error) {
	return proc.Spec{Command: os.Args[0], Env: []string{"MAM_FAKE_COMFY=1", "FAKE_PORT=" + strconv.Itoa(port)}}, nil
}

// file is a model file the fake Hugging Face serves.
type file struct {
	folder, name string
	body         []byte
}

func (f file) sha() string { h := sha256.Sum256(f.body); return hex.EncodeToString(h[:]) }

// fakeHost serves the model files, the engine archive and records the requests.
type fakeHost struct {
	*httptest.Server
	mu      sync.Mutex
	files   map[string]file
	reqs    []string // "path Range"
	auth    []string
	archive []byte
	corrupt map[string]bool // paths served with a flipped byte
}

func newHost(t *testing.T, files ...file) *fakeHost {
	t.Helper()
	h := &fakeHost{files: map[string]file{}, corrupt: map[string]bool{}}
	var err error
	h.archive, err = os.ReadFile(archivePath)
	require.NoError(t, err)
	for _, f := range files {
		h.files["/"+f.folder+"/"+f.name] = f
	}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.reqs = append(h.reqs, r.URL.Path+" "+r.Header.Get("Range"))
		h.auth = append(h.auth, r.Header.Get("Authorization"))
		f, ok := h.files[r.URL.Path]
		corrupt := h.corrupt[r.URL.Path]
		h.mu.Unlock()
		body := h.archive
		if r.URL.Path == "/engine.7z" {
			ok = true
		} else {
			body = append([]byte(nil), f.body...)
			if corrupt {
				body[0] ^= 0xff
			}
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "f", time.Time{}, strings.NewReader(string(body)))
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *fakeHost) requests() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.reqs...)
}

// models of the test catalog: a (1 file), b and c (two files each, sharing the VAE).
var (
	ckptA   = file{"checkpoints", "a.safetensors", []byte(strings.Repeat("A", 200_000))}
	unetB   = file{"diffusion_models", "b.safetensors", []byte(strings.Repeat("B", 300_000))}
	vaeBC   = file{"vae", "shared.safetensors", []byte(strings.Repeat("V", 100_000))}
	clipC   = file{"text_encoders", "c.safetensors", []byte(strings.Repeat("C", 150_000))}
	allTest = []file{ckptA, unetB, vaeBC, clipC}
)

func testCatalog(h *fakeHost) *catalog.Catalog {
	lic := func(id string, commercial bool) catalog.License {
		return catalog.License{ID: id, Version: "1", Name: id, URL: "https://example.com/" + id, Summary: "s", CommercialUse: commercial}
	}
	cf := func(f file) catalog.File {
		return catalog.File{Name: f.name, Folder: f.folder, URL: h.URL + "/" + f.folder + "/" + f.name, SHA256: f.sha(), Size: int64(len(f.body))}
	}
	vram := catalog.VRAM{MinGB: 8, Source: "test", Estimate: true}
	return &catalog.Catalog{
		Engine: catalog.Engine{
			Version: "v1.0.0", SourceURL: "https://github.com/Comfy-Org/ComfyUI/tree/v1.0.0", ArchiveRoot: "ComfyUI_windows_portable",
			Python: pythonExe, Main: "ComfyUI/main.py",
			Variants: map[catalog.Variant]catalog.Asset{
				catalog.VariantNVIDIA: {Label: "nvidia", URL: h.URL + "/engine.7z", SHA256: sumOf(h.archive), Size: int64(len(h.archive)), ExtractedSize: 1 << 20},
			},
		},
		Models: []catalog.Model{
			{ID: "m-a", Name: "A", Kind: localcfg.MediaImage, Tier: catalog.TierStandard, Description: "d", Files: []catalog.File{cf(ckptA)}, VRAM: vram,
				License: lic("apache-2.0", true), Workflows: []catalog.Workflow{{Name: "wf-a", Template: "txt2img-sdxl"}, {Name: "wf-a-i2i", Template: "img2img-sdxl"}}},
			{ID: "m-b", Name: "B", Kind: localcfg.MediaVideo, Tier: catalog.TierStandard, Description: "d", Files: []catalog.File{cf(unetB), cf(vaeBC)}, VRAM: vram,
				License: lic("apache-2.0", true), Workflows: []catalog.Workflow{{Name: "wf-b", Template: "txt2video-wan22-5b"}}},
			{ID: "m-c", Name: "C", Kind: localcfg.MediaImage, Tier: catalog.TierAdvanced, Description: "d", Files: []catalog.File{cf(clipC), cf(vaeBC)}, VRAM: vram,
				License: lic("non-commercial", false), ConsentRequired: true, Workflows: []catalog.Workflow{{Name: "wf-c", Template: "txt2img-flux-schnell"}}},
		},
	}
}

func sumOf(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type rig struct {
	t       *testing.T
	svc     *modelmanager.Service
	queue   *jobqueue.Queue
	table   *repository.JobRepository
	models  *localmodels.Service
	sets    *modelsettings.Service
	ad      *modeladapter.Adapter
	keys    *credentialtest.Memory
	host    *fakeHost
	layout  store.Layout
	free    func(string) (uint64, error)
	started bool
}

type option func(*modelmanager.Deps, *rig)

func newRig(t *testing.T, opts ...option) *rig { return build(t, false, opts...) }

// newPinnedRig is a rig whose model setting file was pinned by the operator (--model-config).
func newPinnedRig(t *testing.T, opts ...option) *rig { return build(t, true, opts...) }

func build(t *testing.T, pinned bool, opts ...option) *rig {
	t.Helper()
	host := newHost(t, allTest...)
	r := &rig{t: t, host: host, keys: credentialtest.New()}
	r.layout = store.Layout{Root: filepath.Join(t.TempDir(), "install", "comfyui")}
	r.free = func(string) (uint64, error) { return 1 << 40, nil }

	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	r.table = repository.NewJobRepository(db)
	r.queue = jobqueue.New(r.table, jobqueue.Options{Workers: 2})

	modelsPath := filepath.Join(t.TempDir(), "models.json")
	if pinned {
		require.NoError(t, os.WriteFile(modelsPath, []byte("{}"), 0o600))
	}
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	bo := localbackends.Options{StagingDir: t.TempDir(), PollEvery: 10 * time.Millisecond}
	r.ad = modeladapter.New(cfg, localbackends.Build(cfg, r.keys, bo))
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	r.models = localmodels.New(localmodels.Deps{Path: modelsPath, Pinned: pinned, Keys: r.keys, Adapter: r.ad, Backends: bo})
	r.sets = modelsettings.New(modelsPath, pinned)
	r.sets.SetOnChange(func() { require.NoError(t, r.models.Reload()) })

	anyURL := func(*url.URL) error { return nil }
	d := modelmanager.Deps{
		Layout: r.layout, Catalog: testCatalog(host), Jobs: r.queue, JobTable: r.table, Models: r.models, Adapter: r.ad,
		Keys: r.keys, Backends: bo, Platform: "windows", FreeBytes: func(p string) (uint64, error) { return r.free(p) },
		Fetch:   fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, "")},
		Command: fakeEngine, PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second,
		Comfy: func(context.Context) detect.Report { return detect.Report{Found: []detect.Found{}} },
		GPU: func(context.Context) gpu.Report {
			return gpu.Report{Recommendation: gpu.Recommendation{Variant: catalog.VariantNVIDIA, Confidence: "high", Reason: "test"}}
		},
	}
	for _, o := range opts {
		o(&d, r)
	}
	r.svc, err = modelmanager.New(d)
	require.NoError(t, err)
	r.svc.Register()
	t.Cleanup(func() { r.queue.Stop(); r.svc.Close() })
	return r
}

// start runs the queue.
func (r *rig) start() {
	r.t.Helper()
	if !r.started {
		r.started = true
		r.queue.Start(context.Background())
	}
}

// wait returns the status of the job once it ended (polling with an upper limit).
func (r *rig) wait(jobID string) modelmanager.JobStatus {
	r.t.Helper()
	r.start()
	var st modelmanager.JobStatus
	require.Eventually(r.t, func() bool {
		var err error
		st, err = r.svc.JobStatus(context.Background(), jobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}

func (r *rig) installEngine() {
	r.t.Helper()
	ref, err := r.svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
	require.NoError(r.t, err)
	st := r.wait(ref.JobID)
	require.Equal(r.t, domain.JobSucceeded, st.State, st.Error)
}

func (r *rig) installModel(id string) modelmanager.JobStatus {
	r.t.Helper()
	ref, err := r.svc.InstallModel(context.Background(), id, nil)
	require.NoError(r.t, err)
	return r.wait(ref.JobID)
}

func (r *rig) path(f file) string { return r.layout.ModelPath("models/" + f.folder + "/" + f.name) }

func (r *rig) exists(f file) bool {
	_, err := os.Stat(r.path(f))
	return err == nil
}

func (r *rig) bundledModels() []string {
	r.t.Helper()
	v, err := r.models.View()
	require.NoError(r.t, err)
	var ids []string
	for _, m := range v.Models {
		if m.Endpoint == modelmanager.BundledEndpoint {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

func ctx() context.Context { return context.Background() }

// listenOn takes a loopback port (the engine finds it busy).
func listenOn(t *testing.T, port int) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	require.NoError(t, err)
	return l
}
