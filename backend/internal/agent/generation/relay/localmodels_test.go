package relay_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/generation/relay"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

const (
	plannerSpec = `{"shot_objective":"Character turns and starts running","duration":"4.0s",
		"character":{"id":"Character_A","identity":"locked","wardrobe":"inherit_previous","hair":"inherit_previous"},
		"acting":[{"time":"0.0-2.0","action":"turn head right"},{"time":"2.0-4.0","action":"begin running"}],
		"camera":{"lens":"35mm","framing":"medium","movement":"slow_push_in"},"lighting":{"mood":"cold blue"},
		"continuity":{"start_from_previous_final_state":true},"constraints":["preserve_face_identity"]}`
	directorPlan = `{"references":[
		{"kind":"character","name":"Mio","prompt":"A young woman in a black coat, full body"},
		{"kind":"keyframe","prompt":"The woman in the black coat on a rainy street, medium shot"}]}`
	extractorDiff = `{"changes":[{"target":"camera","field":"movement","value":"static"}]}`
)

// reasoner answers the Reasoning LLM agents with canned JSON.
type reasoner struct{}

func (reasoner) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	text := map[llm.Agent]string{llm.AgentPromptPlanner: plannerSpec, llm.AgentArtDirector: directorPlan, llm.AgentStateExtractor: extractorDiff}[agent]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

type chat struct{}

func (chat) Announce(context.Context, string, domain.ChatMessage) error { return nil }

type jobs struct{ list []domain.Job }

func (q *jobs) Enqueue(_ context.Context, kind domain.JobKind, payload any) (domain.Job, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, err
	}
	job := domain.Job{ID: domain.NewID(), Kind: kind, Payload: raw, Status: domain.JobQueued}
	q.list = append(q.list, job)
	return job, nil
}

type progressLog struct {
	mu   sync.Mutex
	list []generation.Progress
}

func (p *progressLog) add(pr generation.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, pr)
}

func (p *progressLog) all() []generation.Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]generation.Progress(nil), p.list...)
}

// localConfig routes images and video to models on one ComfyUI endpoint.
func localConfig(t *testing.T, url string) modeladapter.Config {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"gpu": {Kind: localcfg.KindComfyUI, BaseURL: url}}
	cfg.LocalModels = map[string]localcfg.Model{}
	for key, tmpl := range map[string]string{"gpu/txt": "txt2img-sdxl", "gpu/edit": "img2img-sdxl", "gpu/clip": "img2video-wan22-5b"} {
		m, ok := localcfg.TemplateByID(tmpl)
		require.True(t, ok)
		cfg.LocalModels[key] = m.Model
	}
	txt, edit := modeladapter.ImageTarget{Model: "comfyui:gpu/txt"}, modeladapter.ImageTarget{Model: "comfyui:gpu/edit"}
	cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{Primary: txt, Fallback: txt}
	cfg.Images[modeladapter.PurposeEditReference] = modeladapter.ImageRoute{Primary: edit, Fallback: edit}
	clip := modeladapter.VideoTarget{Model: "comfyui:gpu/clip", Resolution: "480p"}
	cfg.Policies[domain.QualityBalanced] = modeladapter.PolicyRoute{Primary: clip, Fallback: clip, Takes: 1}
	require.NoError(t, configcheck.Validate(cfg))
	return cfg
}

// A local model takes the whole path of a cloud one: the Reference Pack's images and the Take's video come
// from the ComfyUI endpoint, the Keyframe reaches the workflow as the first frame, take.model_name and the
// Cost Ledger carry the identifier (a row is written although the price is 0), and the progress of the
// workflow reaches the Shot's notifications with the Takes counts of the Orchestrator.
func TestLocalModelsRunReferencePackAndTakeEndToEnd(t *testing.T) {
	ctx := context.Background()
	comfy := comfyuitest.New(t)
	cfg := localConfig(t, comfy.URL())
	backends := localbackends.Build(cfg, credentialtest.New(), localbackends.Options{PollEvery: time.Millisecond, StagingDir: t.TempDir()})
	adapter := modeladapter.New(cfg, backends)
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })

	db := testdb.Open(t)
	projects, plans, gens := repository.NewProjectRepository(db), repository.NewPlanRepository(db), repository.NewGenerationRepository(db)
	projectID := domain.NewID()
	require.NoError(t, projects.Create(ctx, repository.NewProject{
		Project: domain.Project{ID: projectID, State: domain.StatePlanning, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
		Brief:    domain.Brief{Subject: "美容液"},
		Messages: []domain.ChatMessage{{ProjectID: projectID, Role: domain.RoleUser, Kind: domain.KindText, Content: "美容液のCMを作りたい"}},
	}))
	require.NoError(t, plans.SavePlan(ctx, projectID,
		domain.ScenePlan{Scenes: []domain.PlannedScene{{Title: "Street", Shots: []domain.PlannedShot{{Objective: "主人公が走り出す", DurationSeconds: 4}}}}},
		domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "x"}}}}))
	scenes, err := plans.ListScenes(ctx, projectID)
	require.NoError(t, err)
	shots, err := plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	shotID := shots[0].ID

	log, queue := &progressLog{}, &jobs{}
	progress := relay.New(log.add)
	orch := generation.New(generation.Deps{
		Generations: gens, World: repository.NewWorldRepository(db), Projects: projects, Policies: repository.NewPolicyRepository(db),
		Assets: repository.NewAssetRepository(db), Repairs: repository.NewRepairRepository(db), Chat: chat{}, Jobs: queue, LLM: reasoner{},
		Adapter: adapter, AssetsDir: t.TempDir(), FreeBytes: func(string) (uint64, error) { return 1 << 40, nil }, Notify: progress.Notify,
	})
	handle := progress.Wrap(orch.Handle)
	run := func(start func() (domain.Job, error)) {
		job, err := start()
		require.NoError(t, err)
		require.NoError(t, handle(ctx, job))
	}

	run(func() (domain.Job, error) { return orch.StartReferencePack(ctx, shotID) })
	refs, err := gens.GetShot(ctx, shotID)
	require.NoError(t, err)
	require.Len(t, refs.Shot.References(), 2)
	for _, r := range refs.Shot.References() {
		assert.Contains(t, []domain.ModelName{"comfyui:gpu/txt", "comfyui:gpu/edit"}, r.Model)
	}
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'comfyui:gpu/txt' AND shot_id = ?`, shotID))
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'comfyui:gpu/edit' AND shot_id = ?`, shotID),
		"the Keyframe is made from the references: the edit model")
	assert.Equal(t, 1, comfy.Count("POST /upload/image"), "the reference image is uploaded to ComfyUI")

	run(func() (domain.Job, error) { return orch.Start(ctx, shotID, "") })
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM take WHERE model_name = 'comfyui:gpu/clip'`))
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'comfyui:gpu/clip' AND estimated_cost = 0`),
		"a free model still gets its ledger row")
	assert.Equal(t, 2, comfy.Count("POST /upload/image"), "the Keyframe is uploaded as the first frame")
	assert.Equal(t, 3, comfy.Count("POST /prompt"))

	var percents int
	for _, p := range log.all() {
		if strings.Contains(p.Message, "%）") {
			percents++
			assert.Equal(t, generation.StageGenerating, p.Stage)
			assert.NotEmpty(t, p.ShotID)
		}
	}
	assert.Positive(t, percents, "the workflow's progress reaches the Shot: %+v", log.all())
	last := log.all()[len(log.all())-1]
	assert.Equal(t, generation.StageDone, last.Stage)
	assert.NotContains(t, last.Message, "%）")
}

func countRows(t *testing.T, db *repository.DB, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.Reader().QueryRow(q, args...).Scan(&n))
	return n
}
