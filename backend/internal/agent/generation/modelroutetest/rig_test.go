// Package modelroutetest tests that the Orchestrator chooses the models of a generation by what can be used now (Issue #1113): the start refuses before a
// job is queued when nothing can be used, a default assignment runs on the models that match, and an assignment the user set is never replaced.
package modelroutetest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute/routetest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

var (
	fakePNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	fakeMP4 = append([]byte("\x00\x00\x00\x18ftypisom"), bytes.Repeat([]byte{0}, 32)...)
)

const (
	plannerSpec = `{"shot_objective":"Character notices something behind her and starts running","duration":"4.0s",
		"character":{"id":"Character_A","identity":"locked","wardrobe":"inherit_previous","hair":"inherit_previous"},
		"acting":[{"time":"0.0-4.0","action":"begin running"}],
		"camera":{"lens":"35mm","framing":"medium","movement":"slow_push_in"},"lighting":{"mood":"cold blue"},
		"continuity":{"start_from_previous_final_state":true},"constraints":["preserve_face_identity"]}`
	directorPlan = `{"references":[
		{"kind":"character","name":"Mio","prompt":"A young woman in a black coat, full body"},
		{"kind":"style","prompt":"Cold blue palette with fine film grain"},
		{"kind":"keyframe","prompt":"The woman in the black coat on the rainy street, medium shot"}]}`
	extractorDiff = `{"changes":[{"target":"camera","field":"movement","value":"static"}]}`
)

// calls records which model each call went to, in order.
type calls struct {
	mu     sync.Mutex
	images []domain.ModelName
	videos []domain.ModelName
}

func (c *calls) image(m domain.ModelName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.images = append(c.images, m)
}

func (c *calls) video(m domain.ModelName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.videos = append(c.videos, m)
}

func (c *calls) snapshot() (images, videos []domain.ModelName) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.ModelName(nil), c.images...), append([]domain.ModelName(nil), c.videos...)
}

// backend is the image and video backend of one model: it answers every call and says which model it is.
type backend struct {
	model domain.ModelName
	log   *calls
}

func (b backend) GenerateImage(context.Context, string, modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	b.log.image(b.model)
	return modeladapter.ImageResult{Data: fakePNG, MimeType: "image/png"}, nil
}

func (b backend) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	b.log.video(b.model)
	return modeladapter.VideoResult{File: modeladapter.BlobOf(fakeMP4), MimeType: "video/mp4"}, nil
}

type reasoner struct{}

func (reasoner) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	text := map[llm.Agent]string{llm.AgentPromptPlanner: plannerSpec, llm.AgentArtDirector: directorPlan, llm.AgentStateExtractor: extractorDiff}[agent]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

type chatFake struct {
	mu   sync.Mutex
	msgs []domain.ChatMessage
}

func (c *chatFake) Announce(_ context.Context, _ string, m domain.ChatMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, m)
	return nil
}

func (c *chatFake) all() []domain.ChatMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.ChatMessage(nil), c.msgs...)
}

func (c *chatFake) text() string {
	var out string
	for _, m := range c.all() {
		out += m.Content + "\n"
	}
	return out
}

// jobsFake records the enqueued jobs; the test runs them with Handle.
type jobsFake struct {
	mu   sync.Mutex
	jobs []domain.Job
}

func (q *jobsFake) Enqueue(_ context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, err
	}
	job := domain.Job{ID: fmt.Sprintf("job-%d", len(q.jobs)+1), Kind: kind, Payload: raw, Status: domain.JobQueued}
	q.jobs = append(q.jobs, job)
	return job, nil
}

func (q *jobsFake) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

// rig is one Project with one Shot, an Orchestrator over the observed machine of routetest, and a temp SQLite DB.
type rig struct {
	orch      *generation.Orchestrator
	adapter   *modeladapter.Adapter
	db        *repository.DB
	gens      *repository.GenerationRepository
	policies  *repository.PolicyRepository
	log       *calls
	chat      *chatFake
	jobs      *jobsFake
	projectID string
	shotID    string
	// usable says which models can be used now; a test changes it as the machine changes.
	usable func(domain.ModelName) modelfilter.Availability
}

// newRig builds the rig. config changes the Model Adapter setting (the observed local models are in it); it may be nil.
func newRig(t *testing.T, config func(*modeladapter.Config)) *rig {
	t.Helper()
	routetest.Publish(t)
	ctx := context.Background()
	db := testdb.Open(t)
	projects, plans, policies, gens := repository.NewProjectRepository(db), repository.NewPlanRepository(db), repository.NewPolicyRepository(db), repository.NewGenerationRepository(db)

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

	r := &rig{db: db, gens: gens, policies: policies, log: &calls{}, chat: &chatFake{}, jobs: &jobsFake{}, projectID: projectID, shotID: shots[0].ID, usable: routetest.NoAPIKey()}
	cfg := routetest.LocalConfig()
	if config != nil {
		config(&cfg)
	}
	backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range domain.ImageModels() {
		backends.Image[m] = backend{m, r.log}
	}
	for _, m := range domain.VideoModels() {
		backends.Video[m] = backend{m, r.log}
	}
	r.adapter = modeladapter.New(cfg, backends, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
	r.adapter.Usable.SetAvailability(func(id string) (modelfilter.Availability, bool) { return r.usable(domain.ModelName(id)), true })
	r.orch = generation.New(generation.Deps{
		Generations: gens, World: repository.NewWorldRepository(db), Projects: projects, Policies: policies,
		Assets: repository.NewAssetRepository(db), Repairs: repository.NewRepairRepository(db), Chat: r.chat, Jobs: r.jobs, LLM: reasoner{},
		Adapter: r.adapter, AssetsDir: filepath.Join(t.TempDir(), "assets"), FreeBytes: func(string) (uint64, error) { return 1 << 40, nil },
	})
	return r
}

// nothingUsable is the judgement of a machine on which no model can be used.
func nothingUsable(domain.ModelName) modelfilter.Availability {
	return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません（設定の「API キー」）", "/settings/api-keys")
}

// only is the judgement of a machine on which the listed models, and no other, can be used.
func only(models ...domain.ModelName) func(domain.ModelName) modelfilter.Availability {
	return func(m domain.ModelName) modelfilter.Availability {
		for _, u := range models {
			if u == m {
				return modelfilter.Usable()
			}
		}
		return nothingUsable(m)
	}
}

func (r *rig) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, r.db.Reader().QueryRow(q, args...).Scan(&n))
	return n
}

// runPack starts the Reference Pack and runs its job like the queue does.
func (r *rig) runPack(t *testing.T) error {
	t.Helper()
	job, err := r.orch.StartReferencePack(context.Background(), r.shotID)
	require.NoError(t, err)
	return r.orch.Handle(context.Background(), job)
}

// runGenerate starts the generation of the Shot and runs its job.
func (r *rig) runGenerate(t *testing.T) error {
	t.Helper()
	job, err := r.orch.Start(context.Background(), r.shotID, "")
	require.NoError(t, err)
	return r.orch.Handle(context.Background(), job)
}

func (r *rig) shot(t *testing.T) repository.ShotRef {
	t.Helper()
	ref, err := r.gens.GetShot(context.Background(), r.shotID)
	require.NoError(t, err)
	return ref
}

// takeUses reads the model use every Take of the Shot recorded.
func (r *rig) takeUses(t *testing.T) []modelreg.Use {
	t.Helper()
	rows, err := r.db.Reader().Query(`SELECT t.model_name, t.model_parameters FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, r.shotID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []modelreg.Use
	for rows.Next() {
		var name, params string
		require.NoError(t, rows.Scan(&name, &params))
		var p struct {
			ModelUse modelreg.Use `json:"model_use"`
		}
		require.NoError(t, json.Unmarshal([]byte(params), &p))
		require.Equal(t, name, p.ModelUse.Model, "the Take names the model that made it")
		out = append(out, p.ModelUse)
	}
	require.NoError(t, rows.Err())
	return out
}
