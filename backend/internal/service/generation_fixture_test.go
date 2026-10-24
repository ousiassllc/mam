package service_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

const (
	specAnswer = `{"shot_objective":"Character notices something behind her and starts running","duration":"4.0s",
		"character":{"id":"Character_A","identity":"locked","wardrobe":"inherit_previous","hair":"inherit_previous"},
		"acting":[{"time":"0.0-2.0","action":"turn head right"},{"time":"2.0-4.0","action":"begin running"}],
		"camera":{"lens":"35mm","framing":"medium","movement":"slow_push_in"},"lighting":{"mood":"cold blue"},
		"continuity":{"start_from_previous_final_state":true},"constraints":["preserve_face_identity"]}`
	artAnswer = `{"references":[
		{"kind":"character","name":"Mio","prompt":"A young woman in a black coat, full body, neutral background"},
		{"kind":"product","name":"Serum","prompt":"A glass serum bottle, front view, sharp label","use_uploaded":false},
		{"kind":"location","name":"Street","prompt":"An empty rainy street at dusk, wide view"},
		{"kind":"style","prompt":"Cold blue palette with fine film grain"},
		{"kind":"lighting","prompt":"Soft key light from the left on a simple subject"},
		{"kind":"keyframe","prompt":"The woman in the black coat on the rainy street, medium shot"}]}`
)

// genReasoner answers the Prompt Planner and the Art Director with canned JSON.
type genReasoner struct {
	mu    sync.Mutex
	users map[llm.Agent][]string
	// extractor answers the State Extractor from its prompt; nil means an unusable answer.
	extractor func(user string) string
	// interpreter answers the Command Interpreter from its prompt; nil means no operation.
	interpreter func(user string) string
	// interpreterErr, when set, is the Command Interpreter's failed call.
	interpreterErr error
	// planner, when set, answers the Prompt Planner instead of specAnswer.
	planner func(user string) string
	// director answers the Director Agent (Continue) from its prompt; nil means an empty answer.
	director func(system, user string) string
	// editor answers the Editor Judge; nil means an empty answer.
	editor string
	// editorHold, when set, runs before the Editor Judge is answered; its error is the call's.
	editorHold func(ctx context.Context) error
}

func (g *genReasoner) Complete(ctx context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	if agent == llm.AgentEditor && g.editorHold != nil {
		if err := g.editorHold(ctx); err != nil {
			return llm.Response{}, err
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.users == nil {
		g.users = map[llm.Agent][]string{}
	}
	g.users[agent] = append(g.users[agent], req.User)
	text := map[llm.Agent]string{llm.AgentPromptPlanner: specAnswer, llm.AgentArtDirector: artAnswer}[agent]
	if agent == llm.AgentStateExtractor && g.extractor != nil {
		text = g.extractor(req.User)
	}
	if agent == llm.AgentPromptPlanner && g.planner != nil {
		text = g.planner(req.User)
	}
	if agent == llm.AgentCommandInterpreter {
		if g.interpreterErr != nil {
			return llm.Response{}, g.interpreterErr
		}
		text = `{"operations":[]}`
		if g.interpreter != nil {
			text = g.interpreter(req.User)
		}
	}
	if agent == llm.AgentEditor {
		text = g.editor
	}
	if agent == llm.AgentDirector && g.director != nil {
		text = g.director(req.System, req.User)
	}
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// prompts is a copy of the user prompts the agent received: Jobs still run in the background, so a
// test must not read g.users directly.
func (g *genReasoner) prompts(agent llm.Agent) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.users[agent]...)
}

// reset forgets what the agent received so far.
func (g *genReasoner) reset(agent llm.Agent) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.users == nil {
		g.users = map[llm.Agent][]string{}
	}
	g.users[agent] = nil
}

func (g *genReasoner) calls(agent llm.Agent) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.users[agent])
}

// modelFake is the image and video backend of every model: no external API is called.
type modelFake struct {
	mu       sync.Mutex
	err      error
	block    chan struct{} // when set, holds every image and video call until it is closed
	actual   *float64      // the cost the vendor reports for a video, when set
	requests []modeladapter.VideoRequest
	images   []string
	inflight atomic.Int32
}

func (m *modelFake) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	m.inflight.Add(1)
	defer m.inflight.Add(-1)
	if err := m.hold(ctx); err != nil {
		return modeladapter.VideoResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	if m.err != nil {
		return modeladapter.VideoResult{}, m.err
	}
	return modeladapter.VideoResult{File: modeladapter.BlobOf(mp4Bytes), MimeType: "video/mp4", ActualCost: m.actual}, nil
}

func (m *modelFake) GenerateImage(ctx context.Context, model string, _ modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	if err := m.hold(ctx); err != nil {
		return modeladapter.ImageResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.images = append(m.images, model)
	if m.err != nil {
		return modeladapter.ImageResult{}, m.err
	}
	return modeladapter.ImageResult{Data: pngBytes, MimeType: "image/png"}, nil
}

// hold waits for the gate to open, so a test can keep a Job from ending until it has looked at the
// state the action answered with.
func (m *modelFake) hold(ctx context.Context) error {
	if m.block == nil {
		return nil
	}
	select {
	case <-m.block:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *modelFake) videoRequests() []modeladapter.VideoRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]modeladapter.VideoRequest(nil), m.requests...)
}

type genFixture struct {
	fixture
	llm       *genReasoner
	models    *modelFake
	projectID string
	sceneID   string
	shotID    string
}

func newGenFixture(t *testing.T, withModels bool) genFixture {
	t.Helper()
	return newGenFixtureWithFreeBytes(t, withModels, func(string) (uint64, error) { return 1 << 40, nil })
}

func newGenFixtureWithFreeBytes(t *testing.T, withModels bool, freeBytes func(string) (uint64, error)) genFixture {
	t.Helper()
	return newGenFixtureWithDeps(t, withModels, freeBytes, nil)
}

// newGenFixtureWithDeps is newGenFixtureWithFreeBytes with a last look at the Deps (nil: none).
func newGenFixtureWithDeps(t *testing.T, withModels bool, freeBytes func(string) (uint64, error), tweak func(*service.Deps)) genFixture {
	t.Helper()
	db := testdb.Open(t)

	fake, reasoner := &modelFake{}, &genReasoner{}
	backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range domain.ImageModels() {
		backends.Image[m] = fake
	}
	for _, m := range domain.VideoModels() {
		backends.Video[m] = fake
	}
	noSleep := func(context.Context, time.Duration) error { return nil }
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	deps := service.Deps{DB: db, AssetsDir: filepath.Join(t.TempDir(), "assets"), LLM: reasoner, FreeBytes: freeBytes, Jobs: jobs}
	if withModels {
		deps.Models = modeladapter.New(modeladapter.DefaultConfig(), backends, modeladapter.WithSleep(noSleep), modeladapter.WithTimeLimit(shortLimit))
	}
	if tweak != nil {
		tweak(&deps)
	}
	svc := servicetest.New(t, deps)
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })

	f := genFixture{fixture: fixture{svc: svc, db: db, assetsDir: deps.AssetsDir}, llm: reasoner, models: fake}
	p := f.createProject(t, "美容液のCMを作りたい")
	f.projectID = p.ID
	sceneView, err := svc.Plans.CreateScene(ctx, p.ID, service.SceneForm{Title: "Street"})
	require.NoError(t, err)
	f.sceneID = sceneView.Scene.ID
	shotView, err := svc.Plans.CreateShot(ctx, f.sceneID, service.ShotForm{Objective: "主人公が振り返って走り出す", DurationSeconds: "4"})
	require.NoError(t, err)
	f.shotID = shotView.Shots[0].ID
	return f
}

func (f genFixture) shot(t *testing.T) service.ShotFragment {
	t.Helper()
	frag, err := f.svc.Generation.Shot(context.Background(), f.shotID)
	require.NoError(t, err)
	return frag
}

func (f genFixture) waitShotState(t *testing.T, want domain.ShotState) {
	t.Helper()
	require.Eventually(t, func() bool { return f.shot(t).State == want }, 30*time.Second, 10*time.Millisecond)
}

// waitJobsDone waits until every progress card of the thread shows a finished job.
func (f genFixture) waitJobsDone(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		seen := 0
		for _, m := range f.thread(t, f.projectID) {
			if m.Kind != domain.KindProgress {
				continue
			}
			seen++
			if m.Job == nil || (m.Job.Status != domain.JobSucceeded && m.Job.Status != domain.JobFailed) {
				return false
			}
		}
		return seen > 0
	}, 30*time.Second, 10*time.Millisecond, "the generate job never finished")
}

func (f genFixture) query(t *testing.T, q string, args ...any) *sql.Row {
	t.Helper()
	return f.db.Reader().QueryRow(q, args...)
}

func (f genFixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.query(t, q, args...).Scan(&n))
	return n
}

func (f genFixture) setBudget(t *testing.T, project, shot *float64) {
	t.Helper()
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE project SET project_budget_ceiling = ?, shot_budget_ceiling = ? WHERE id = ?`, project, shot, f.projectID)
		return err
	}))
}
