package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
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
	// planner, when set, answers the Prompt Planner instead of specAnswer.
	planner func(user string) string
	// director answers the Director Agent (Continue) from its prompt; nil means an empty answer.
	director func(system, user string) string
	// editor answers the Editor Judge; nil means an empty answer.
	editor string
}

func (g *genReasoner) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
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

func (g *genReasoner) calls(agent llm.Agent) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.users[agent])
}

// modelFake is the image and video backend of every model: no external API is called.
type modelFake struct {
	mu       sync.Mutex
	err      error
	block    chan struct{}
	actual   *float64 // the cost the vendor reports for a video, when set
	requests []modeladapter.VideoRequest
	images   []string
	inflight atomic.Int32
}

func (m *modelFake) GenerateVideo(ctx context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	m.inflight.Add(1)
	defer m.inflight.Add(-1)
	if m.block != nil {
		select {
		case <-m.block:
		case <-ctx.Done():
			return modeladapter.VideoResult{}, ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	if m.err != nil {
		return modeladapter.VideoResult{}, m.err
	}
	return modeladapter.VideoResult{File: modeladapter.BlobOf(mp4Bytes), MimeType: "video/mp4", ActualCost: m.actual}, nil
}

func (m *modelFake) GenerateImage(_ context.Context, model string, _ modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.images = append(m.images, model)
	if m.err != nil {
		return modeladapter.ImageResult{}, m.err
	}
	return modeladapter.ImageResult{Data: pngBytes, MimeType: "image/png"}, nil
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
	dir := t.TempDir()
	db, err := repository.Open(context.Background(), filepath.Join(dir, "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	fake, reasoner := &modelFake{}, &genReasoner{}
	backends := modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{}, Video: map[domain.ModelName]modeladapter.VideoBackend{}}
	for _, m := range domain.ImageModels() {
		backends.Image[m] = fake
	}
	for _, m := range domain.VideoModels() {
		backends.Video[m] = fake
	}
	noSleep := func(context.Context, time.Duration) error { return nil }
	deps := service.Deps{DB: db, AssetsDir: filepath.Join(dir, "assets"), LLM: reasoner, FreeBytes: freeBytes}
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	deps.Jobs = jobs
	if withModels {
		deps.Models = modeladapter.New(modeladapter.DefaultConfig(), backends, modeladapter.WithSleep(noSleep))
	}
	svc := service.New(deps)
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

var japanese = regexp.MustCompile(`[\p{Han}\p{Hiragana}\p{Katakana}]`)

func TestGenerateShot_ReturnsAcceptedGeneratingShotAndMakesTakesWithCanonicalIdentifiers(t *testing.T) {
	f := newGenFixture(t, true)

	frag, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	assert.Equal(t, domain.ShotGenerating, frag.State, "the answer shows the Shot generating; the work runs in the background")

	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)

	// F-06: the Canonical Shot Spec is in shot.shot_spec and, as a snapshot, in prompt_version.
	shot := f.shot(t).Shot
	spec, ok := shot.CanonicalSpec()
	require.True(t, ok)
	assert.Equal(t, "Character_A", spec.Character.ID)
	assert.Equal(t, "4.0s", spec.Duration)
	var snapshot, stored string
	require.NoError(t, f.query(t, `SELECT shot_spec_snapshot FROM prompt_version WHERE shot_id = ?`, f.shotID).Scan(&snapshot))
	require.NoError(t, f.query(t, `SELECT shot_spec FROM shot WHERE id = ?`, f.shotID).Scan(&stored))
	assert.JSONEq(t, stored, snapshot)

	// F-07 / F-12: the balanced policy makes 3 Takes with Seedance 2.0, stored with canonical identifiers.
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'seedance-2.0' AND status = 'candidate'`))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'seedance-2.0' AND shot_id = ? AND estimated_cost > 0`, f.shotID))
	takes := f.shot(t).Takes
	require.Len(t, takes, 3)
	for _, tk := range takes {
		require.NotEmpty(t, tk.FilePath)
		_, err := os.Stat(filepath.Join(f.assetsDir, filepath.FromSlash(tk.FilePath)))
		assert.NoError(t, err, "the video file of a Take exists")
	}

	// The prompt sent to the model is English, and the record keeps what each model would get.
	for _, req := range f.models.videoRequests() {
		assert.NotRegexp(t, japanese, req.Prompt)
		assert.Contains(t, req.Prompt, "starts running")
	}
	var record string
	require.NoError(t, f.query(t, `SELECT model_adapter_output FROM prompt_version WHERE shot_id = ?`, f.shotID).Scan(&record))
	assert.Contains(t, record, `"seedance-2.0"`)
	assert.Contains(t, record, `"kling-3.0"`, "the alternative model's request is recorded too")

	// The chat shows a progress card and a card per Take.
	kinds := map[domain.MessageKind]int{}
	for _, m := range f.thread(t, f.projectID) {
		kinds[m.Kind]++
	}
	assert.Equal(t, 1, kinds[domain.KindProgress])
	assert.Equal(t, 3, kinds[domain.KindTake])
}

func TestGenerateShot_KeepsTheSpecUnlessThereIsANewInstruction(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	_, err := f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
	assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner))

	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.count(t, `SELECT COUNT(*) FROM take`) == 6 }, 30*time.Second, 10*time.Millisecond)
	f.waitShotState(t, domain.ShotPlanning)
	assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner), "a retake reuses the Canonical Shot Spec")
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM prompt_version`), "every generation is a prompt version")

	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "もう少しカメラを寄せて")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.count(t, `SELECT COUNT(*) FROM take`) == 9 }, 30*time.Second, 10*time.Millisecond)
	assert.Equal(t, 2, f.llm.calls(llm.AgentPromptPlanner), "an instruction makes the Prompt Planner revise the spec")
	assert.Contains(t, f.llm.users[llm.AgentPromptPlanner][1], "もう少しカメラを寄せて")
}

func TestGenerateShot_RejectsASecondRunWhileTheShotIsBeingGenerated(t *testing.T) {
	f := newGenFixture(t, true)
	f.models.block = make(chan struct{})
	ctx := context.Background()

	_, err := f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBusy)
	_, err = f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	assert.ErrorIs(t, err, service.ErrBusy, "a Reference Pack is not built while the Shot is generating either")

	close(f.models.block)
	f.waitJobsDone(t)
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`), "only one generation ran")
}

func TestGenerateShot_BudgetCeilingLimitsTakesOrRefuses(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	// Balanced: Seedance 2.0 at 720p costs 0.08 USD/s; the Shot's clip is 4 s, so one Take costs 0.32.
	tiny, oneTake := 0.01, 0.35

	f.setBudget(t, nil, &tiny)
	_, err := f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBudgetExceeded, "not even one Take fits")
	assert.Equal(t, domain.ShotPlanning, f.shot(t).State, "nothing was started")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job`))

	f.setBudget(t, nil, &oneTake)
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM take`), "the number of Takes is reduced to what the budget allows")
	var record string
	require.NoError(t, f.query(t, `SELECT model_adapter_output FROM prompt_version WHERE shot_id = ?`, f.shotID).Scan(&record))
	assert.Contains(t, record, "budget allows 1 of 3 Takes")

	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	assert.ErrorIs(t, err, service.ErrBudgetExceeded, "the first generation used up the Shot's budget")
}

func TestGenerateShot_FailureIsReportedInTheChatAndTheShotIsReleased(t *testing.T) {
	f := newGenFixture(t, true)
	f.models.err = &modeladapter.ProviderError{Vendor: "seedance", Status: http.StatusForbidden, Message: "policy"}

	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)

	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take`))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry`), "a failed call is not charged")
	last := lastMessage(t, f.thread(t, f.projectID))
	assert.Equal(t, domain.KindText, last.Kind)
	assert.Contains(t, last.Content, "生成に失敗しました")
	assert.Contains(t, last.Content, "拒否")

	f.models.err = nil
	_, err = f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	assert.NoError(t, err, "the user can start again")
}

func TestBuildReferencePack_MakesEveryReferenceAndTheKeyframeThenGenerationUsesThem(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	frag, err := f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	require.NoError(t, err)
	assert.Equal(t, domain.ShotGenerating, frag.State)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)

	refs := f.shot(t).References()
	var kinds []domain.ReferenceKind
	for _, r := range refs {
		kinds = append(kinds, r.Kind)
		assert.NotEmpty(t, r.AssetID)
	}
	assert.Equal(t, domain.ReferenceKinds(), kinds, "Character / Product / Location / Style / Lighting References and the Keyframe")
	assert.Equal(t, 6, f.count(t, `SELECT COUNT(*) FROM asset WHERE type = 'reference'`))

	// model-adapters.md §3: new Character / Product / Location -> Imagen 4; Style / Lighting / Keyframe -> Nano Banana.
	byKind := map[domain.ReferenceKind]domain.ModelName{}
	for _, r := range refs {
		byKind[r.Kind] = r.Model
	}
	assert.Equal(t, domain.ModelImagen4, byKind[domain.RefCharacter])
	assert.Equal(t, domain.ModelImagen4, byKind[domain.RefProduct])
	assert.Equal(t, domain.ModelImagen4, byKind[domain.RefLocation])
	assert.Equal(t, domain.ModelNanoBanana, byKind[domain.RefStyle])
	assert.Equal(t, domain.ModelNanoBanana, byKind[domain.RefLighting])
	assert.Equal(t, domain.ModelNanoBanana, byKind[domain.RefKeyframe])
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'imagen-4'`))
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = 'nano-banana'`))

	keyframes := 0
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindKeyframe {
			keyframes++
		}
	}
	assert.Equal(t, 6, keyframes, "every image is shown in the chat")

	// The Keyframe starts the clip and the other references go along.
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	f.waitShotState(t, domain.ShotPlanning)
	require.Eventually(t, func() bool { return len(f.models.videoRequests()) == 3 }, 30*time.Second, 10*time.Millisecond)
	for _, req := range f.models.videoRequests() {
		require.NotNil(t, req.FirstFrame, "the Keyframe is the first frame")
		assert.Equal(t, pngBytes, req.FirstFrame.Data)
	}
	var stored string
	require.NoError(t, f.query(t, `SELECT shot_spec FROM shot WHERE id = ?`, f.shotID).Scan(&stored))
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stored), &doc))
	assert.Contains(t, doc, "references", "the Canonical Shot Spec is written next to the Reference Pack")
	assert.Contains(t, doc, "shot_objective")
}

func TestBuildReferencePack_FailureKeepsTheOldPackAndLeavesNoFiles(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	_, err := f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
	before := f.shot(t).References()
	files := countFiles(t, f.assetsDir)

	f.models.err = &modeladapter.ProviderError{Vendor: "google", Status: http.StatusForbidden}
	_, err = f.svc.Generation.BuildReferencePack(ctx, f.shotID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return f.shot(t).State == domain.ShotPlanning && f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'failed'`) == 1
	}, 30*time.Second, 10*time.Millisecond)

	assert.Equal(t, before, f.shot(t).References())
	assert.Equal(t, files, countFiles(t, f.assetsDir))
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	require.NoError(t, filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	}))
	return n
}

func TestSubscribeShot_SaysIdleWhenNothingRunsAndStreamsProgressUntilDone(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()

	ch, cancel, err := f.svc.Generation.SubscribeShot(ctx, f.shotID)
	require.NoError(t, err)
	assert.Equal(t, generation.StageIdle, (<-ch).Stage, "a client that connects to an idle Shot is told so at once")
	cancel()

	f.models.block = make(chan struct{})
	_, err = f.svc.Generation.GenerateShot(ctx, f.shotID, "")
	require.NoError(t, err)
	ch, cancel, err = f.svc.Generation.SubscribeShot(ctx, f.shotID)
	require.NoError(t, err)
	defer cancel()
	close(f.models.block)

	var stages []generation.Stage
	deadline := time.After(30 * time.Second)
	for last := generation.Stage(""); last != generation.StageDone; {
		select {
		case p := <-ch:
			assert.Equal(t, f.shotID, p.ShotID)
			stages = append(stages, p.Stage)
			last = p.Stage
		case <-deadline:
			t.Fatalf("no done message; got %v", stages)
		}
	}
	assert.Contains(t, stages, generation.StageGenerating)
}

func TestSubscribeShot_RepairingShotIsNotToldIdle(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE shot SET state = 'REPAIRING' WHERE id = ?`, f.shotID)
		return err
	}))

	ch, cancel, err := f.svc.Generation.SubscribeShot(ctx, f.shotID)
	require.NoError(t, err)
	defer cancel()

	select {
	case p := <-ch:
		t.Fatalf("a Shot being repaired must not start with a message, got stage %q", p.Stage)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSubscribeShot_UnknownShotIsNotFound(t *testing.T) {
	f := newGenFixture(t, true)
	_, _, err := f.svc.Generation.SubscribeShot(context.Background(), "nope")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Generation.GenerateShot(context.Background(), "nope", "")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestGenerateShot_IsUnavailableWithoutAModelAdapter(t *testing.T) {
	f := newGenFixture(t, false)
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	assert.ErrorIs(t, err, service.ErrUnavailable)
	_, err = f.svc.Generation.BuildReferencePack(context.Background(), f.shotID)
	assert.ErrorIs(t, err, service.ErrUnavailable)
}

func TestGenerateShot_RejectsAnOverlongInstruction(t *testing.T) {
	f := newGenFixture(t, true)
	long := make([]rune, 1001)
	for i := range long {
		long[i] = 'あ'
	}
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, string(long))
	var v *service.ValidationError
	require.ErrorAs(t, err, &v)
	assert.Equal(t, "additional_instruction", v.Field)
}

func TestRecover_ReleasesShotsThatWereBeingGeneratedWhenTheAppStopped(t *testing.T) {
	f := newGenFixture(t, true)
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE shot SET state = 'GENERATING' WHERE id = ?`, f.shotID)
		return err
	}))

	n, err := f.svc.Recover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, domain.ShotPlanning, f.shot(t).State)
}

var characterIDRe = regexp.MustCompile(`character id="([^"]+)"`)

// extractAnswer makes the State Extractor say Mio is anxious at the hallway door and the camera is static.
func extractAnswer(user string) string {
	m := characterIDRe.FindStringSubmatch(user)
	if m == nil {
		return `{"changes":[{"target":"camera","field":"movement","value":"static"}]}`
	}
	return `{"changes":[
		{"target":"character","id":"` + m[1] + `","field":"emotion","value":"anxious"},
		{"target":"character","id":"` + m[1] + `","field":"position","value":"hallway_door"},
		{"target":"camera","field":"movement","value":"static"},
		{"target":"story","field":"previous_event","value":"phone_call_ended"}]}`
}

func (f genFixture) buildReferencePack(t *testing.T) {
	t.Helper()
	_, err := f.svc.Generation.BuildReferencePack(context.Background(), f.shotID)
	require.NoError(t, err)
	f.waitJobsDone(t)
	f.waitShotState(t, domain.ShotPlanning)
}

func (f genFixture) generateAndWait(t *testing.T, shotID string, jobs int) {
	t.Helper()
	_, err := f.svc.Generation.GenerateShot(context.Background(), shotID, "")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'succeeded' AND kind = 'generate'`) == jobs
	}, 30*time.Second, 10*time.Millisecond)
}

func TestGenerateShot_StateExtractorAppendsWorldStateVersionAndTheNextShotStartsFromIt(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.buildReferencePack(t)

	f.generateAndWait(t, f.shotID, 2) // the Reference Pack job counts as the first

	// F-10: a State Diff and a new world_state_version were appended for the Shot.
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE project_id = ?`, f.projectID))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM state_diff WHERE shot_id = ? AND take_id IN (SELECT id FROM take)`, f.shotID))
	var diff string
	require.NoError(t, f.query(t, `SELECT diff FROM state_diff WHERE shot_id = ?`, f.shotID).Scan(&diff))
	assert.Contains(t, diff, `"field":"emotion"`)
	assert.Contains(t, diff, `"to":"anxious"`)
	world, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotEmpty(t, world.World.Characters)
	assert.Equal(t, "anxious", world.World.Characters[0].Emotion)
	assert.Equal(t, "hallway_door", world.World.Characters[0].Position)
	assert.Equal(t, "static", world.World.Camera.Movement)
	assert.Equal(t, 1, world.World.Story.Scene, "the Shot is in the first Scene")
	assert.Equal(t, "phone_call_ended", world.World.Story.PreviousEvent)

	// F-09 / F-10: the next Shot's Prompt Planner input carries that state.
	shot2, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"})
	require.NoError(t, err)
	f.generateAndWait(t, shot2.Shots[1].ID, 3)
	planner := f.llm.users[llm.AgentPromptPlanner]
	require.Len(t, planner, 2)
	for _, want := range []string{`emotion="anxious"`, `position="hallway_door"`, `movement="static"`, "phone_call_ended"} {
		assert.Contains(t, planner[1], want)
	}
	assert.NotContains(t, planner[0], "anxious", "the first Shot had no earlier state")
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM world_state_version WHERE project_id = ?`, f.projectID), "versions are appended, never overwritten")
}

func TestGenerateShot_StateExtractorLeavesLockedFieldsAlone(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.buildReferencePack(t)
	var charID string
	require.NoError(t, f.query(t, `SELECT id FROM character WHERE project_id = ?`, f.projectID).Scan(&charID))
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES ('l1', ?, 'character', ?, 'emotion', 'lock', 'null')`, f.projectID, charID)
		return err
	}))
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE character SET emotion = 'calm' WHERE id = ?`, charID)
		return err
	}))

	f.generateAndWait(t, f.shotID, 2)

	world, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, "calm", world.World.Characters[0].Emotion, "the LOCKed emotion is not updated")
	assert.Equal(t, "hallway_door", world.World.Characters[0].Position, "unlocked fields still follow the Shot")
	var diff string
	require.NoError(t, f.query(t, `SELECT diff FROM state_diff WHERE shot_id = ?`, f.shotID).Scan(&diff))
	assert.NotContains(t, diff, `"emotion"`, "LOCKed fields are excluded from the State Diff")
}

func TestGenerateShot_StateExtractorFailureDoesNotFailTheGeneration(t *testing.T) {
	f := newGenFixture(t, true) // the fake reasoner gives the State Extractor an unusable answer

	f.generateAndWait(t, f.shotID, 1)

	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take`), "the Takes are kept")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM world_state_version`), "the World State simply stays as it was")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'failed'`))
}
