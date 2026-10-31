// Package mmtest is a model manager Service over fakes, for the tests of the layers above it (the screen's handlers, the
// MCP tools): a fake GitHub / Hugging Face that serves the engine archive and the model files, a fake ComfyUI engine
// process, a temporary directory next to the "exe", and the job queue. The fake engine is this test binary itself: a
// package that starts the engine calls Main from its TestMain. Not for production code.
package mmtest

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/detect"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// Rig is the Service and what it runs on.
type Rig struct {
	T   *testing.T
	Svc *modelmanager.Service
	// Gentest is the generation test service the trial generations run on, over the same database, queue and Model Adapter.
	Gentest *gentest.Service
	Queue   *jobqueue.Queue
	Table   *repository.JobRepository
	Models  *localmodels.Service
	Sets    *modelsettings.Service
	Adapter *modeladapter.Adapter
	Keys    *credentialtest.Memory
	Host    *Host
	// HF is the fake Hugging Face of the import from Hugging Face (no repository until a test adds one); the Service reads it
	// and the token goes to it as to huggingface.co.
	HF      *hftest.Server
	Layout  store.Layout
	DataDir string
	// Free is the free space the Service sees (default: plenty); a test lowers it.
	Free func(string) (uint64, error)
	// GPUReport is what the GPU detection answers.
	GPUReport gpu.Report
	started   bool
	db        *repository.DB
	pinned    bool
	authHost  string
	hfBase    string
}

// Option changes the Deps (and the Rig) before the Service is made.
type Option func(*modelmanager.Deps, *Rig)

// WithDB makes the rig use an existing database (the one an MCP test environment reads its jobs from).
func WithDB(db *repository.DB) Option { return func(_ *modelmanager.Deps, r *Rig) { r.db = db } }

// WithLogger gives the Service the logger a test reads back (a test that the Hugging Face token is not logged).
func WithLogger(l *slog.Logger) Option { return func(d *modelmanager.Deps, _ *Rig) { d.Logger = l } }

// Pinned makes the model setting file the one the operator pinned (--model-config).
func Pinned() Option { return func(_ *modelmanager.Deps, r *Rig) { r.pinned = true } }

// HFAuth makes the fake Hugging Face (and the fake hosts, which share the loopback address) the host that gets the stored
// token, as huggingface.co is in production: a test of a gated repository needs it. Without it no host sees the token.
func HFAuth() Option { return func(_ *modelmanager.Deps, r *Rig) { r.authHost = "127.0.0.1" } }

// HFBase makes the Service ask the site at base for Hugging Face's API, instead of the fake of the rig (a test of how a site that
// fails, limits or is gone is told to the user). The file URLs of the models stay as they were made.
func HFBase(base string) Option { return func(_ *modelmanager.Deps, r *Rig) { r.hfBase = base } }

// NotWindows makes the Service one of an OS without the bundled engine.
func NotWindows() Option { return func(d *modelmanager.Deps, _ *Rig) { d.Platform = "linux" } }

// New builds a Rig. Its engine archive and models are those of the test catalog (m-a, m-b, m-c). The Queue is not
// started until Start or Wait.
func New(t *testing.T, opts ...Option) *Rig {
	t.Helper()
	host := newHost(t)
	r := &Rig{T: t, Host: host, HF: hftest.New(t), Keys: credentialtest.New(), DataDir: t.TempDir()}
	r.Layout = store.Layout{Root: filepath.Join(t.TempDir(), "install", "comfyui")}
	r.Free = func(string) (uint64, error) { return 1 << 40, nil }
	r.GPUReport = gpu.Report{Recommendation: gpu.Recommendation{Variant: catalog.VariantNVIDIA, Confidence: "high", Reason: "test GPU"}}

	d := modelmanager.Deps{
		Layout: r.Layout, Catalog: testCatalog(host), Keys: r.Keys, Platform: "windows",
		Command: fakeEngine, PollEvery: 20 * time.Millisecond, StartTimeout: 20 * time.Second,
		Comfy: func(context.Context) detect.Report { return detect.Report{Found: []detect.Found{}} },
	}
	for _, o := range opts {
		o(&d, r)
	}
	if r.db == nil {
		r.db = testdb.Open(t)
	}
	r.Table = repository.NewJobRepository(r.db)
	r.Queue = jobqueue.New(r.Table, jobqueue.Options{Workers: 2})

	modelsPath := filepath.Join(t.TempDir(), "models.json")
	if r.pinned {
		require.NoError(t, os.WriteFile(modelsPath, []byte("{}"), 0o600))
	}
	cfg, err := configcheck.Load("")
	require.NoError(t, err)
	bo := localbackends.Options{
		StagingDir: t.TempDir(), PollEvery: 10 * time.Millisecond,
		// as the app wires it: the bundled engine starts when a model of it is called
		Prepare: func(ctx context.Context, endpoint string) error {
			if r.Svc != nil {
				return r.Svc.Prepare(ctx, endpoint)
			}
			return nil
		},
	}
	r.Adapter = modeladapter.New(cfg, localbackends.Build(cfg, r.Keys, bo))
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	r.Models = localmodels.New(localmodels.Deps{Path: modelsPath, Pinned: r.pinned, Keys: r.Keys, Adapter: r.Adapter, Backends: bo})
	r.Sets = modelsettings.New(modelsPath, r.pinned)
	r.Sets.SetOnChange(func() { require.NoError(t, r.Models.Reload()) })

	anyURL := func(*url.URL) error { return nil }
	d.Jobs, d.JobTable, d.Models, d.Adapter, d.Backends = r.Queue, r.Table, r.Models, r.Adapter, bo
	d.FreeBytes = func(p string) (uint64, error) { return r.Free(p) }
	d.GPU = func(context.Context) gpu.Report { return r.GPUReport }
	d.Fetch = fetch.Options{CheckURL: anyURL, Client: fetch.WithRedirectRules(&http.Client{}, anyURL, r.authHost)}
	d.Fetch.AuthHost = r.authHost
	d.HFBase = r.HF.URL
	if r.hfBase != "" {
		d.HFBase = r.hfBase
	}
	r.Svc, err = modelmanager.New(d)
	require.NoError(t, err)
	r.Svc.Register()
	// as the app wires it: an adult model is refused by every generation and left out of the assignment choices while the gate is closed
	r.Adapter.Gate.Set(func(m domain.ModelName) bool { return r.Svc.HidesModel(string(m)) })
	r.Sets.SetHidden(r.Svc.HidesModel)
	r.Gentest = gentest.New(gentest.Deps{
		Repo: gentestrepo.New(r.db), Jobs: r.Queue, JobTable: r.Table, Adapter: r.Adapter, DataDir: r.DataDir, AssetsDir: t.TempDir(),
		Projects: repository.NewProjectRepository(r.db), Assets: repository.NewAssetRepository(r.db), References: repository.NewReferenceRepository(r.db),
		Hidden: r.Svc.HidesModel,
	})
	r.Gentest.Register()
	t.Cleanup(func() { r.Queue.Stop(); r.Svc.Close() })
	return r
}

// Start runs the job queue.
func (r *Rig) Start() {
	r.T.Helper()
	if !r.started {
		r.started = true
		r.Queue.Start(context.Background())
	}
}

// Wait returns the status of the job once it ended (polling with an upper limit).
func (r *Rig) Wait(jobID string) modelmanager.JobStatus {
	r.T.Helper()
	r.Start()
	var st modelmanager.JobStatus
	require.Eventually(r.T, func() bool {
		var err error
		st, err = r.Svc.JobStatus(context.Background(), jobID)
		return err == nil && (st.State == domain.JobSucceeded || st.State == domain.JobFailed)
	}, 30*time.Second, 20*time.Millisecond, "the job did not end")
	return st
}

// InstallEngine installs the engine of the test catalog and waits for it.
func (r *Rig) InstallEngine() {
	r.T.Helper()
	ref, err := r.Svc.InstallEngine(context.Background(), modelmanager.EngineInstallInput{Variant: catalog.VariantNVIDIA})
	require.NoError(r.T, err)
	st := r.Wait(ref.JobID)
	require.Equal(r.T, domain.JobSucceeded, st.State, st.Error)
}

// InstallModel installs a model (accepting its license when it needs one) and waits for it.
func (r *Rig) InstallModel(id string) modelmanager.JobStatus {
	r.T.Helper()
	m, err := r.Svc.Model(id)
	require.NoError(r.T, err)
	var accept *modelmanager.LicenseRef
	if m.ConsentRequired {
		accept = &modelmanager.LicenseRef{ID: m.License.ID, Version: m.License.Version}
	}
	ref, err := r.Svc.InstallModel(context.Background(), id, accept)
	require.NoError(r.T, err)
	return r.Wait(ref.JobID)
}

// ConfirmAdult opens the age gate as a user who confirmed would (the confirmation of the current wording).
func (r *Rig) ConfirmAdult() {
	r.T.Helper()
	require.NoError(r.T, r.Svc.SetAdultContent(true, &modelmanager.AdultConfirmation{Adult: true, NoticeVersion: modelmanager.AdultNoticeVersion}))
}

// ModelFile is where the installed file lies (for a test that checks the disk).
func (r *Rig) ModelFile(f File) string {
	return r.Layout.ModelPath("models/" + f.Folder + "/" + f.Name)
}

// DB is the database the rig runs on (a test that adds a service over the same database, as the saved workflows).
func (r *Rig) DB() *repository.DB { return r.db }
