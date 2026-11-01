package generation_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

func (p *progressLog) last() generation.Progress {
	all := p.all()
	if len(all) == 0 {
		return generation.Progress{}
	}
	return all[len(all)-1]
}

func (p *progressLog) stages() []generation.Stage {
	var out []generation.Stage
	for _, pr := range p.all() {
		out = append(out, pr.Stage)
	}
	return out
}

type options struct {
	withJudge bool
	// videoModels are the video models that have a backend; empty: every model.
	videoModels []domain.ModelName
	quality     domain.QualityPolicy
	freeBytes   func(string) (uint64, error)
	limits      *domain.PolicyLimits
	config      func(*modeladapter.Config) // changes the Model Adapter setting
}

// fixture is one Project with one Shot and an Orchestrator wired to fakes and a temp SQLite DB.
type fixture struct {
	orch      *generation.Orchestrator
	adapter   *modeladapter.Adapter
	db        *repository.DB
	gens      *repository.GenerationRepository
	policies  *repository.PolicyRepository
	llm       *reasoner
	video     *videoFake
	image     *imageFake
	judge     *judgeFake
	chat      *chatFake
	jobs      *jobsFake
	progress  *progressLog
	assetsDir string
	projectID string
	shotID    string
}

func newFixture(t *testing.T, opt options) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db := testdb.Open(t)

	projects := repository.NewProjectRepository(db)
	plans := repository.NewPlanRepository(db)
	policies := repository.NewPolicyRepository(db)
	gens := repository.NewGenerationRepository(db)

	projectID := domain.NewID()
	require.NoError(t, projects.Create(ctx, repository.NewProject{
		Project: domain.Project{
			ID: projectID, State: domain.StatePlanning, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		},
		Brief:    domain.Brief{Subject: "美容液"},
		Messages: []domain.ChatMessage{{ProjectID: projectID, Role: domain.RoleUser, Kind: domain.KindText, Content: "美容液のCMを作りたい"}},
	}))
	require.NoError(t, plans.SavePlan(ctx, projectID,
		domain.ScenePlan{Scenes: []domain.PlannedScene{{Title: "Street", Shots: []domain.PlannedShot{{Objective: "主人公が振り返って走り出す", DurationSeconds: 4}}}}},
		domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "x"}}}}))
	scenes, err := plans.ListScenes(ctx, projectID)
	require.NoError(t, err)
	shots, err := plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	if opt.quality != "" || opt.limits != nil {
		q := opt.quality
		if q == "" {
			q = domain.QualityBalanced
		}
		require.NoError(t, policies.SetQualityPolicy(ctx, projectID, q, opt.limits))
	}

	f := &fixture{
		db: db, gens: gens, policies: policies, llm: &reasoner{}, video: &videoFake{}, image: &imageFake{},
		chat: &chatFake{}, jobs: &jobsFake{}, progress: &progressLog{},
		assetsDir: filepath.Join(dir, "assets"), projectID: projectID, shotID: shots[0].ID,
	}
	backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range domain.ImageModels() {
		backends.Image[m] = f.image
	}
	videoModels := opt.videoModels
	if len(videoModels) == 0 {
		videoModels = domain.VideoModels()
	}
	for _, m := range videoModels {
		backends.Video[m] = f.video
	}
	noSleep := func(context.Context, time.Duration) error { return nil }
	freeBytes := opt.freeBytes
	if freeBytes == nil {
		freeBytes = func(string) (uint64, error) { return 1 << 40, nil }
	}
	cfg := pricedConfig()
	if opt.config != nil {
		opt.config(&cfg)
	}
	f.adapter = modeladapter.New(cfg, backends, modeladapter.WithSleep(noSleep))
	deps := generation.Deps{
		Generations: gens, World: repository.NewWorldRepository(db), Projects: projects, Policies: policies,
		Assets: repository.NewAssetRepository(db), Repairs: repository.NewRepairRepository(db), Chat: f.chat, Jobs: f.jobs, LLM: f.llm,
		Adapter:   f.adapter,
		AssetsDir: f.assetsDir, FreeBytes: freeBytes, Notify: f.progress.add,
	}
	if opt.withJudge {
		f.judge = &judgeFake{}
		deps.Judge = f.judge
	}
	f.orch = generation.New(deps)
	return f
}

// run queues the job with start and runs it like the job queue does.
func (f *fixture) run(ctx context.Context, t *testing.T, start func() (domain.Job, error)) error {
	t.Helper()
	job, err := start()
	require.NoError(t, err)
	return f.orch.Handle(ctx, job)
}

// generate runs a whole generate job.
func (f *fixture) generate(t *testing.T, instruction string) error {
	t.Helper()
	return f.run(context.Background(), t, func() (domain.Job, error) {
		return f.orch.Start(context.Background(), f.shotID, instruction)
	})
}

func (f *fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.db.Reader().QueryRow(q, args...).Scan(&n))
	return n
}

func (f *fixture) ref(t *testing.T) repository.ShotRef {
	t.Helper()
	ref, err := f.gens.GetShot(context.Background(), f.shotID)
	require.NoError(t, err)
	return ref
}

func (f *fixture) shotState(t *testing.T) domain.ShotState { return f.ref(t).Shot.State }

// storedFiles counts the files under the assets directory.
func (f *fixture) storedFiles(t *testing.T) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(f.assetsDir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
	}
	return n
}

// pricedConfig is the default Model Adapter setting with a price for GPT Image 2, which has none by default
// (model-adapters.md §9.2): the tests count the Cost Ledger rows that carry an estimated cost.
func pricedConfig() modeladapter.Config {
	cfg := modeladapter.DefaultConfig()
	cfg.Models[domain.ModelGPTImage2] = modeladapter.ModelConfig{Model: "gpt-image-2", CostPerImage: 0.05}
	return cfg
}
