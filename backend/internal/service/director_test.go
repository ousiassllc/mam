package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// plannerRewrite is what the Prompt Planner answers to a direction: it changes the character's
// wardrobe, the acting, the camera and the lighting all at once, as an unconstrained model may.
const plannerRewrite = `{"shot_objective":"Character notices something behind her and starts running","duration":"4.0s",
	"character":{"id":"Character_A","identity":"locked","wardrobe":"red_dress","hair":"inherit_previous"},
	"acting":[{"time":"0.0-4.0","action":"sprint in panic"}],
	"camera":{"lens":"85mm","framing":"close-up","movement":"handheld_shake"},"lighting":{"mood":"warm sunset"},
	"continuity":{"start_from_previous_final_state":true},"constraints":["preserve_face_identity"]}`

func (f genFixture) specOf(t *testing.T, shotID string) domain.ShotSpec {
	t.Helper()
	frag, err := f.svc.Generation.Shot(context.Background(), shotID)
	require.NoError(t, err)
	spec, ok := frag.CanonicalSpec()
	require.True(t, ok, "the Shot has a Canonical Shot Spec")
	return spec
}

func (f genFixture) takes(t require.TestingT) int { return f.count(t, `SELECT COUNT(*) FROM take`) }

func (f genFixture) lockRows(t *testing.T) int { return f.count(t, `SELECT COUNT(*) FROM lock`) }

// firstGeneration makes the Shot's first Takes (and its spec) and returns once the Shot is idle.
func (f genFixture) firstGeneration(t *testing.T) {
	t.Helper()
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 3, f.takes(c)) }, 30*time.Second, 10*time.Millisecond)
	f.waitShotState(t, domain.ShotPlanning)
}

func (f genFixture) waitTakeCount(t *testing.T, n int) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, n, f.takes(c)) }, 30*time.Second, 10*time.Millisecond)
	f.waitShotState(t, domain.ShotPlanning)
}

func (f genFixture) setLock(t *testing.T, p domain.LockParameter, mode domain.LockMode) {
	t.Helper()
	_, err := f.svc.Director.SetShotLock(context.Background(), f.shotID, p, mode, "")
	require.NoError(t, err)
}

func opsJSON(ops ...string) func(string) string {
	return func(string) string { return `{"operations":[` + strings.Join(ops, ",") + `]}` }
}

func TestSetMode_IsKeptAndRecordedInTheThread(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "美容液のCMを作りたい")
	require.Equal(t, domain.ModeAuto, p.Mode)

	got, err := f.svc.Director.SetMode(context.Background(), p.ID, domain.ModeDirector)
	require.NoError(t, err)
	assert.Equal(t, domain.ModeDirector, got.Mode)
	reloaded, err := f.svc.Projects.Get(context.Background(), p.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ModeDirector, reloaded.Mode, "the mode is kept")
	last := f.thread(t, p.ID)
	assert.Contains(t, last[len(last)-1].Content, "DIRECTOR MODE")
	_, err = f.svc.Director.SetMode(context.Background(), p.ID, "manual")
	var ve *service.ValidationError
	require.ErrorAs(t, err, &ve)
	_, err = f.svc.Director.SetMode(context.Background(), "nope", domain.ModeAuto)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestSetMode_DirectorHoldsThePipelineAndAutoResumesIt(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.createProject(t, "美容液の縦型15秒CMを作りたい")
	ctx := context.Background()
	_, err := f.svc.Director.SetMode(ctx, p.ID, domain.ModeDirector)
	require.NoError(t, err)

	f.completeBrief(t, p.ID)
	f.waitIdle(t, p.ID)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, domain.StateDraft, f.state(t, p.ID), "DIRECTOR MODE: the AI does not go on by itself")
	assert.Zero(t, f.llm.count(llm.AgentProducer))
	_, err = f.svc.Director.SetMode(ctx, p.ID, domain.ModeAuto)
	require.NoError(t, err)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	assert.Equal(t, 1, f.llm.count(llm.AgentProducer), "back in AUTO the following stages resume")
}

func TestSetShotLock_SetsEveryParameterAndFreezesTheCurrentVideoModel(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	ctx := context.Background()

	for _, p := range domain.ShotLockParameters() {
		locks, err := f.svc.Director.SetShotLock(ctx, f.shotID, p, domain.LockManual, "seedance-2.0")
		require.NoError(t, err, p)
		assert.Equal(t, domain.LockManual, locks.Mode(p), p)
	}
	locks, err := f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamCamera, domain.LockAuto, "")
	require.NoError(t, err)
	assert.Equal(t, domain.LockAuto, locks.Mode(domain.ParamCamera))
	assert.Equal(t, len(domain.ShotLockParameters()), f.lockRows(t), "one row per parameter: the lock table is the single place")

	locks, err = f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamVideoModel, domain.LockLocked, "")
	require.NoError(t, err)
	assert.True(t, locks.Locked(domain.ParamVideoModel))
	var model string
	require.NoError(t, f.query(t, `SELECT model_name FROM take ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&model))
	assert.Equal(t, model, locks.Text(domain.ParamVideoModel), "LOCK freezes the model of the latest Take")

	_, err = f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamVideoModel, domain.LockLocked, "kling-3.0")
	assert.ErrorIs(t, err, service.ErrLocked, "a LOCKed value is not replaced")
	locks, err = f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamVideoModel, domain.LockAuto, "")
	require.NoError(t, err)
	assert.Equal(t, domain.LockAuto, locks.Mode(domain.ParamVideoModel))
}

func TestSetShotLock_RejectsWhatCannotBeSet(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	for name, run := range map[string]func() error{
		"project parameter on a shot": func() error {
			_, err := f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamStory, domain.LockLocked, "")
			return err
		},
		"unknown parameter": func() error {
			_, err := f.svc.Director.SetShotLock(ctx, f.shotID, "wardrobe", domain.LockLocked, "")
			return err
		},
		"unknown mode": func() error {
			_, err := f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamCamera, "freeze", "")
			return err
		},
		"manual model without a model": func() error {
			_, err := f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamVideoModel, domain.LockManual, "")
			return err
		},
		"unknown model": func() error {
			_, err := f.svc.Director.SetShotLock(ctx, f.shotID, domain.ParamVideoModel, domain.LockManual, "sora")
			return err
		},
		"shot parameter on the project": func() error {
			_, err := f.svc.Director.SetProjectLock(ctx, f.projectID, domain.ParamCamera, domain.LockLocked)
			return err
		},
	} {
		var ve *service.ValidationError
		assert.ErrorAs(t, run(), &ve, name)
	}
	_, err := f.svc.Director.SetShotLock(ctx, "nope", domain.ParamCamera, domain.LockLocked, "")
	assert.ErrorIs(t, err, service.ErrNotFound)
	_, err = f.svc.Director.SetProjectLock(ctx, "nope", domain.ParamStory, domain.LockLocked)
	assert.ErrorIs(t, err, service.ErrNotFound)
	assert.Zero(t, f.lockRows(t), "nothing was written")
}

func TestVideoModel_ManualOrLockForcesThatModelForTheShot(t *testing.T) {
	f := newGenFixture(t, true)
	_, err := f.svc.Director.SetShotLock(context.Background(), f.shotID, domain.ParamVideoModel, domain.LockManual, "kling-3.0")
	require.NoError(t, err)
	f.firstGeneration(t)
	assert.Equal(t, 3, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name = 'kling-3.0'`), "the Balanced default is Seedance; the fixed model wins")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take WHERE model_name <> 'kling-3.0'`))
}

func TestEditSpec_LockedPartsAreRefusedWith423AndNothingIsWritten(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	ctx := context.Background()
	before := f.specOf(t, f.shotID)
	versions := f.count(t, `SELECT COUNT(*) FROM prompt_version`)
	f.setLock(t, domain.ParamCamera, domain.LockLocked)

	edited := before
	edited.Camera.Movement = "handheld_shake"
	edited.Acting = []domain.ActingBeat{{Time: "0.0-4.0", Action: "walk slowly"}}
	text, _ := json.Marshal(edited)
	_, err := f.svc.Director.EditSpec(ctx, f.shotID, string(text))
	require.ErrorIs(t, err, service.ErrLocked)
	var le *service.LockedError
	require.ErrorAs(t, err, &le)
	assert.Equal(t, []domain.LockParameter{domain.ParamCamera}, le.Parameters)
	assert.Equal(t, before, f.specOf(t, f.shotID), "nothing changed, not even the unlocked part")
	assert.Equal(t, versions, f.count(t, `SELECT COUNT(*) FROM prompt_version`))
	edited.Camera = before.Camera
	text, _ = json.Marshal(edited)
	view, err := f.svc.Director.EditSpec(ctx, f.shotID, string(text))
	require.NoError(t, err, "an edit that leaves the LOCKed part alone is stored")
	assert.Equal(t, "walk slowly", view.Spec.Acting[0].Action)
	assert.Equal(t, versions+1, f.count(t, `SELECT COUNT(*) FROM prompt_version`), "the edit is a prompt version")

	f.setLock(t, domain.ParamPrompt, domain.LockLocked)
	edited.Style = "noir"
	text, _ = json.Marshal(edited)
	_, err = f.svc.Director.EditSpec(ctx, f.shotID, string(text))
	assert.ErrorIs(t, err, service.ErrLocked, "Prompt LOCK covers the whole spec")
}

func TestEditSpec_RejectsAnUnusableSpecAndKeepsTheInput(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	ctx := context.Background()
	before := f.specOf(t, f.shotID)

	for name, text := range map[string]string{
		"not json":       `{"shot_objective":`,
		"unknown key":    `{"shot_objective":"x","camera":{"framing":"wide"},"bogus":1}`,
		"no framing":     `{"shot_objective":"x","camera":{}}`,
		"acting outside": `{"shot_objective":"x","camera":{"framing":"wide"},"acting":[{"time":"0.0-9.0","action":"run"}]}`,
		"japanese":       `{"shot_objective":"走る","camera":{"framing":"wide"}}`,
	} {
		view, err := f.svc.Director.EditSpec(ctx, f.shotID, text)
		var ve *service.ValidationError
		require.ErrorAs(t, err, &ve, name)
		assert.Equal(t, text, view.SpecInput, name)
		assert.NotEmpty(t, view.SpecError, name)
	}
	assert.Equal(t, before, f.specOf(t, f.shotID))
	_, err := f.svc.Director.EditSpec(ctx, "nope", `{}`)
	assert.ErrorIs(t, err, service.ErrNotFound)
}

func TestDirectorCommand_RegeneratesOnlyThisShotAndKeepsWhatIsNotChanged(t *testing.T) {
	f := newGenFixture(t, true)
	other, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"})
	require.NoError(t, err)
	f.firstGeneration(t)
	before := f.specOf(t, f.shotID)
	f.llm.planner = func(string) string { return plannerRewrite }
	f.llm.interpreter = opsJSON(`{"type":"regenerate","changes":["camera"],"instruction":"Move the camera closer and keep it steady."}`)

	res, err := f.svc.Director.DirectorCommand(context.Background(), f.shotID, "もう少し寄って")
	require.NoError(t, err)
	assert.True(t, res.Started, "202: a regeneration was queued")
	f.waitTakeCount(t, 6)
	after := f.specOf(t, f.shotID)
	assert.Equal(t, "close-up", after.Camera.Framing, "the direction's part changed")
	assert.Equal(t, before.Character, after.Character, "Character is preserved")
	assert.Equal(t, before.Acting, after.Acting, "Acting is preserved")
	assert.Equal(t, before.Lighting, after.Lighting, "the environment is preserved")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, other.Shots[1].ID), "the other Shot is untouched")
	assert.Contains(t, f.llm.prompts(llm.AgentPromptPlanner)[1], "Fixed parts")
	var texts []string
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindText {
			texts = append(texts, m.Content)
		}
	}
	require.GreaterOrEqual(t, len(texts), 2)
	assert.Equal(t, "もう少し寄って", texts[len(texts)-2], "the direction and the answer are in the thread")
	assert.Contains(t, texts[len(texts)-1], "Shot 1 だけを再生成")
}

// requireAnswerRecordedAfterTheRequestEnded runs call (the direction of a regeneration) with the
// context of a request that is cancelled, as a closed client does, as soon as the job the direction
// queued has announced its progress card (the second message of the thread: the direction itself is
// the first). Events reach the cancelling goroutine asynchronously, so the call may return first;
// the test waits for the cancellation to have happened, or fails if the card was never announced.
func (f genFixture) requireAnswerRecordedAfterTheRequestEnded(t *testing.T, call func(ctx context.Context)) {
	t.Helper()
	f.llm.planner = func(string) string { return plannerRewrite }
	f.llm.interpreter = opsJSON(`{"type":"regenerate","changes":["camera"],"instruction":"Move the camera closer and keep it steady."}`)
	ctx, cancelRequest := context.WithCancel(context.Background())
	t.Cleanup(cancelRequest)
	events, unsubscribe, err := f.svc.Chat.Subscribe(context.Background(), f.projectID)
	require.NoError(t, err)
	t.Cleanup(unsubscribe)
	go func() {
		created := 0
		for e := range events {
			if e.Type == service.EventMessageCreated {
				if created++; created == 2 {
					cancelRequest()
					return
				}
			}
		}
	}()
	call(ctx)
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the request did not end: the progress card of the queued job was never announced")
	}
	assert.Len(t, f.answers(t), 1, "the answer is in the thread")
	f.waitTakeCount(t, 6)
}
