// Package gentesttest is a generation test Service over fake model backends, a real (temporary) database and a running job
// queue, for the tests of the layers above it (the screen's handlers, the MCP tools). Not for production code.
package gentesttest

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// PNG is the bytes the fake image backends return (a valid 1x1 PNG).
var PNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x9a\xa0\xa0\x00\x00\x00\x00IEND\xaeB`\x82")

// Call is one request a fake backend got.
type Call struct {
	Model string
	Image *modeladapter.ImageRequest
	Video *modeladapter.VideoRequest
}

// Fake serves every image and video model of the rig. Err, when set for a model, is the failure of its calls; Block, when set,
// holds the calls of a model until it receives (or the call's context ends).
type Fake struct {
	mu    sync.Mutex
	calls []Call
	err   map[domain.ModelName]error
	block map[domain.ModelName]chan struct{}
	start chan struct{}
}

// Calls returns the requests received so far.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// CallsOf returns the requests received by one model.
func (f *Fake) CallsOf(m domain.ModelName) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if c.Model == string(m) {
			out = append(out, c)
		}
	}
	return out
}

// FailWith makes every call of the model fail with err.
func (f *Fake) FailWith(m domain.ModelName, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err[m] = err
}

// Hold makes the calls of the model wait for a receive on the returned channel; Started receives once per call that began.
func (f *Fake) Hold(m domain.ModelName) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan struct{})
	f.block[m] = ch
	return ch
}

// Started receives a value every time a held call begins.
func (f *Fake) Started() <-chan struct{} { return f.start }

func (f *Fake) enter(ctx context.Context, m domain.ModelName, c Call) error {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	err, ch := f.err[m], f.block[m]
	f.mu.Unlock()
	if ch != nil {
		f.start <- struct{}{}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

type imageBackend struct {
	f *Fake
	m domain.ModelName
}

func (b imageBackend) GenerateImage(ctx context.Context, _ string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	if err := b.f.enter(ctx, b.m, Call{Model: string(b.m), Image: &req}); err != nil {
		return modeladapter.ImageResult{}, err
	}
	return modeladapter.ImageResult{Data: PNG, MimeType: "image/png"}, nil
}

type videoBackend struct {
	f *Fake
	m domain.ModelName
}

func (b videoBackend) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	if err := b.f.enter(ctx, b.m, Call{Model: string(b.m), Video: &req}); err != nil {
		return modeladapter.VideoResult{}, err
	}
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("MP4DATA")), MimeType: "video/mp4"}, nil
}

// Rig is the Service and what it runs on.
type Rig struct {
	T         testing.TB
	Svc       *gentest.Service
	Fake      *Fake
	Adapter   *modeladapter.Adapter
	Queue     *jobqueue.Queue
	DB        *repository.DB
	Repo      *gentestrepo.Repository
	Jobs      *repository.JobRepository
	DataDir   string
	AssetsDir string
	Config    modeladapter.Config
	// Assigned records the calls of Deps.Assign.
	Assigned *[]string
	seed     int64
	missing  map[domain.ModelName]bool
}

// Option changes the Rig before the Service is made.
type Option func(*Rig, *gentest.Deps)

// WithConfig replaces the Model Adapter setting (prices, limits).
func WithConfig(f func(*modeladapter.Config)) Option {
	return func(r *Rig, _ *gentest.Deps) { f(&r.Config) }
}

// WithoutBackend leaves a model without a backend (the model is listed as unavailable).
func WithoutBackend(m domain.ModelName) Option {
	return func(r *Rig, _ *gentest.Deps) { r.missing[m] = true }
}

// New builds a Rig with a started job queue. Every built-in image and video model has a fake backend; the seed of the
// n-th seeded result is 1000+n.
func New(t testing.TB, opts ...Option) *Rig { return NewOn(t, testdb.Open(t), opts...) }

// NewOn is New over a database that exists already (the MCP and screen tests build the rest of the services on it).
func NewOn(t testing.TB, db *repository.DB, opts ...Option) *Rig {
	t.Helper()
	f := &Fake{err: map[domain.ModelName]error{}, block: map[domain.ModelName]chan struct{}{}, start: make(chan struct{}, 64)}
	r := &Rig{T: t, Fake: f, DB: db, DataDir: t.TempDir(), AssetsDir: t.TempDir(), Config: modeladapter.DefaultConfig(), Assigned: new([]string), missing: map[domain.ModelName]bool{}}
	deps := gentest.Deps{
		Repo: gentestrepo.New(db), JobTable: repository.NewJobRepository(db), Projects: repository.NewProjectRepository(db),
		Assets: repository.NewAssetRepository(db), References: repository.NewReferenceRepository(db), DataDir: r.DataDir, AssetsDir: r.AssetsDir,
		Assign: func(_ context.Context, project string, target domain.WorldTarget, id, slot, ref string) error {
			*r.Assigned = append(*r.Assigned, project+"|"+string(target)+"|"+id+"|"+slot+"|"+ref)
			return nil
		},
		Seed: func() int64 { r.seed++; return 1000 + r.seed },
	}
	for _, o := range opts {
		o(r, &deps)
	}
	backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range []domain.ModelName{domain.ModelImagen4, domain.ModelNanoBanana, domain.ModelFlux2, domain.ModelGPTImage2} {
		if !r.missing[m] {
			backends.Image[m] = imageBackend{f, m}
		}
	}
	for _, m := range []domain.ModelName{domain.ModelKling3, domain.ModelSeedance2, domain.ModelRunwayGen45} {
		if !r.missing[m] {
			backends.Video[m] = videoBackend{f, m}
		}
	}
	r.Adapter = modeladapter.New(r.Config, backends, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
	r.Queue = jobqueue.New(deps.JobTable, jobqueue.Options{})
	deps.Adapter, deps.Jobs = r.Adapter, r.Queue
	r.Svc = gentest.New(deps)
	r.Svc.Register()
	r.Repo, r.Jobs = deps.Repo, deps.JobTable
	ctx, cancel := context.WithCancel(context.Background())
	r.Queue.Start(ctx)
	t.Cleanup(func() { r.Queue.Stop(); cancel() })
	return r
}

// Wait blocks until every result of the batch is over (at most 20 seconds) and returns the batch.
func (r *Rig) Wait(batchID string) gentest.BatchStatus {
	r.T.Helper()
	var st gentest.BatchStatus
	require.Eventually(r.T, func() bool {
		var err error
		st, err = r.Svc.Batch(context.Background(), batchID, true)
		require.NoError(r.T, err)
		return st.Finished
	}, 20*time.Second, 10*time.Millisecond, "the batch did not finish")
	return st
}

// Confirmed runs the request with the estimate it has now (what the screen's confirm button sends).
func (r *Rig) Confirmed(req gentest.Request) (gentest.RunRef, error) {
	r.T.Helper()
	est, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(r.T, err)
	return r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
}

// NewProject creates a Project and returns its id.
func (r *Rig) NewProject() string {
	r.T.Helper()
	id := domain.NewID()
	require.NoError(r.T, repository.NewProjectRepository(r.DB).Create(context.Background(), repository.NewProject{
		Project: domain.Project{
			ID: id, State: domain.StatePlanning, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		},
		Brief:    domain.Brief{Subject: "美容液"},
		Messages: []domain.ChatMessage{{ProjectID: id, Role: domain.RoleUser, Kind: domain.KindText, Content: "CM"}},
	}))
	return id
}

// WithAssignError makes the slot assignment of an import fail with err (the refusals of WorldService.AssignReference).
func WithAssignError(err error) Option {
	return func(_ *Rig, d *gentest.Deps) {
		d.Assign = func(context.Context, string, domain.WorldTarget, string, string, string) error { return err }
	}
}

// SeedAsset registers an image asset with a Reference Library entry named Mio in a Project (the file is already on disk under
// the assets directory at rel).
func (r *Rig) SeedAsset(ctx context.Context, projectID, assetID, itemID, rel string) error {
	asset := domain.Asset{ID: assetID, ProjectID: projectID, Type: domain.AssetReference, FilePath: filepath.ToSlash(rel), MimeType: "image/png", SizeBytes: 10}
	item := domain.ReferenceItem{ID: itemID, ProjectID: projectID, AssetID: assetID, Kind: domain.RefCharacter, Name: "Mio", Source: domain.SourceUploaded}
	return r.Svc.References.Add(ctx, item, asset)
}
