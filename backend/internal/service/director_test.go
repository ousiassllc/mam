package service_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func (f genFixture) takes(t *testing.T) int { return f.count(t, `SELECT COUNT(*) FROM take`) }

func (f genFixture) lockRows(t *testing.T) int { return f.count(t, `SELECT COUNT(*) FROM lock`) }

// firstGeneration makes the Shot's first Takes (and its spec) and returns once the Shot is idle.
func (f genFixture) firstGeneration(t *testing.T) {
	t.Helper()
	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.takes(t) == 3 }, 30*time.Second, 10*time.Millisecond)
	f.waitShotState(t, domain.ShotPlanning)
}

func (f genFixture) waitTakeCount(t *testing.T, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return f.takes(t) == n }, 30*time.Second, 10*time.Millisecond)
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

// answers lists the text the assistant gave to the direction in the thread.
func (f genFixture) answers(t *testing.T) []string {
	t.Helper()
	var texts []string
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindText && m.Role == domain.RoleAssistant && strings.Contains(m.Content, "だけを再生成します") {
			texts = append(texts, m.Content)
		}
	}
	return texts
}

// A regeneration was queued (and is paid for) before the client went away: its answer is recorded
// anyway, or the thread would end with the direction alone and a retry would pay a second time.
func TestDirectorCommand_TheAnswerOfAQueuedRegenerationIsRecordedAfterTheRequestEnded(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.requireAnswerRecordedAfterTheRequestEnded(t, func(ctx context.Context) {
		_, _ = f.svc.Director.DirectorCommand(ctx, f.shotID, "もう少し寄って")
	})
}

func TestChat_DirectorModeTheAnswerOfAQueuedRegenerationIsRecordedAfterTheRequestEnded(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.startDirecting(t)
	f.requireAnswerRecordedAfterTheRequestEnded(t, func(ctx context.Context) {
		_, _ = f.svc.Chat.PostMessage(ctx, f.projectID, service.PostMessageInput{Content: "もう少し寄って"})
	})
}

func TestDirectorCommand_DirectionPrecedesTheProgressCardItCausesAndASecondOneWhileItIsRunningIsRefused(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.llm.planner = func(string) string { return plannerRewrite }
	regenerate := opsJSON(`{"type":"regenerate","changes":["camera"],"instruction":"Move the camera closer and keep it steady."}`)
	direct := func() error {
		_, err := f.svc.Director.DirectorCommand(context.Background(), f.shotID, "もう少し寄って")
		return err
	}
	var refused error // the same direction again while it is being read (a double click, a second tab) pays and records nothing (#669)
	f.llm.interpreter = func(user string) string { refused = f.overlap(direct); return regenerate(user) }

	require.NoError(t, direct())

	assert.ErrorIs(t, refused, service.ErrConflict)
	assert.Equal(t, 1, f.llm.calls(llm.AgentCommandInterpreter), "the direction is read once")
	f.waitTakeCount(t, 6)
	// A chat that is open asks only for the messages after the last one it shows; a direction committed
	// after the progress card would be left out of every such sync, whatever its ID. So the direction is
	// listed, and was committed, before the card it caused.
	directionBeforeCard := func(msgs [][3]string) bool { // each message is {role, kind, content}
		direction, card := -1, -1
		for i, m := range msgs {
			if m[0] == string(domain.RoleUser) && m[2] == "もう少し寄って" {
				direction = i
			}
			if m[1] == string(domain.KindProgress) {
				card = i
			}
		}
		require.True(t, direction >= 0 && card >= 0, "the direction and the progress card are in the thread")
		return direction < card
	}
	var listed, committed [][3]string
	for _, m := range f.thread(t, f.projectID) {
		listed = append(listed, [3]string{string(m.Role), string(m.Kind), m.Content})
	}
	rows, err := f.db.Reader().QueryContext(context.Background(), `SELECT role, kind, content FROM chat_message WHERE project_id = ? ORDER BY rowid`, f.projectID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var m [3]string
		require.NoError(t, rows.Scan(&m[0], &m[1], &m[2]))
		committed = append(committed, m)
	}
	require.NoError(t, rows.Err())
	assert.True(t, directionBeforeCard(listed), "the direction is listed before the progress card it caused")
	assert.True(t, directionBeforeCard(committed), "the direction was committed before the progress card")
}

func TestDirectorCommand_LockedPartsSurviveWhateverThePlannerAnswers(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	before := f.specOf(t, f.shotID)
	f.setLock(t, domain.ParamCharacterIdentity, domain.LockLocked)
	f.setLock(t, domain.ParamLighting, domain.LockLocked)
	f.llm.planner = func(string) string { return plannerRewrite }
	f.llm.interpreter = opsJSON(`{"type":"regenerate","changes":["acting","camera"],"instruction":"Make her panic."}`)

	_, err := f.svc.Director.DirectorCommand(context.Background(), f.shotID, "慌てさせて")

	require.NoError(t, err)
	f.waitTakeCount(t, 6)
	after := f.specOf(t, f.shotID)
	assert.Equal(t, before.Character, after.Character, "LOCKed Character Identity is never changed by the AI")
	assert.Equal(t, before.Lighting, after.Lighting)
	assert.Equal(t, "sprint in panic", after.Acting[0].Action, "the unlocked parts follow the direction")
	assert.Equal(t, "close-up", after.Camera.Framing)
}

func TestDirectorCommand_ChangingALockedPartIsRefusedAndNothingIsDone(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.setLock(t, domain.ParamCamera, domain.LockLocked)
	f.llm.interpreter = opsJSON(`{"type":"regenerate","changes":["camera"],"instruction":"Zoom in."}`)
	threadBefore := len(f.thread(t, f.projectID))

	_, err := f.svc.Director.DirectorCommand(context.Background(), f.shotID, "寄って")

	require.ErrorIs(t, err, service.ErrLocked)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 3, f.takes(t), "no regeneration")
	assert.Equal(t, 1, f.llm.calls(llm.AgentPromptPlanner))
	assert.Len(t, f.thread(t, f.projectID), threadBefore, "nothing is recorded")

	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"camera","mode":"auto"}`,
		`{"type":"regenerate","changes":["camera"],"instruction":"Zoom in."}`)
	_, err = f.svc.Director.DirectorCommand(context.Background(), f.shotID, "カメラは任せる。寄って")
	require.NoError(t, err, "unlocking in the same direction makes the change allowed")
	f.waitTakeCount(t, 6)
}

func TestDirectorCommand_PromptLockRefusesANewInstructionOnGenerate(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.setLock(t, domain.ParamPrompt, domain.LockLocked)

	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "もう少し寄って")
	assert.ErrorIs(t, err, service.ErrLocked)
	_, err = f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")
	assert.NoError(t, err, "a plain retake reuses the LOCKed spec")
	f.waitTakeCount(t, 6)
}

func TestDirectorCommand_SetsLocksFromWordsAndNeedsAnInterpreter(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"character_identity","mode":"lock"}`, `{"type":"set_lock","parameter":"nonsense","mode":"lock"}`)

	res, err := f.svc.Director.DirectorCommand(context.Background(), f.shotID, "この顔は好きだから固定")

	require.NoError(t, err)
	assert.False(t, res.Started)
	assert.True(t, res.Inspector.Locks.Locked(domain.ParamCharacterIdentity))
	assert.Equal(t, 1, f.lockRows(t), "an operation on an unknown parameter is dropped")
	assert.Zero(t, f.takes(t), "locking does not generate")

	_, err = f.svc.Director.DirectorCommand(context.Background(), "nope", "x")
	assert.ErrorIs(t, err, service.ErrNotFound, "an unknown Shot is 404")
}

func TestDirectorCommand_AFailedReasoningLLMHasTheDocumentedStatus(t *testing.T) {
	f := newGenFixture(t, true)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		err   error
		check func(t *testing.T, err error)
	}{
		"no API key is 503": {llm.ErrNoAPIKey, func(t *testing.T, err error) { assert.ErrorIs(t, err, service.ErrUnavailable) }},
		"a 429 is rate limited": {&llm.APIError{Provider: llm.ProviderAnthropic, Status: 429}, func(t *testing.T, err error) {
			assert.ErrorIs(t, err, service.ErrRateLimited)
		}},
		"an open circuit breaker is an AI error": {fmt.Errorf("interpret: %w", llm.ErrCircuitOpen), func(t *testing.T, err error) {
			var ai *service.AIError
			require.ErrorAs(t, err, &ai)
			assert.Contains(t, ai.Reason, "一時的に利用できません")
		}},
		"an unusable answer is an AI error": {fmt.Errorf("%w (x)", llm.ErrInvalidOutput), func(t *testing.T, err error) {
			var ai *service.AIError
			assert.ErrorAs(t, err, &ai)
			assert.NotErrorIs(t, err, service.ErrRateLimited)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f.llm.interpreterErr = tc.err
			_, err := f.svc.Director.DirectorCommand(ctx, f.shotID, "カメラを寄せて")
			require.Error(t, err)
			tc.check(t, err)
			assert.Zero(t, f.takes(t))
		})
	}
}

// A partial regeneration needs the Repair Agent, which this fixture (no Frames) does not have: the
// thread tells that the feature is not set up, in DirectorCommand and in the chat alike, not an
// "internal error".
func TestDirectorFailures_TheThreadTellsAServiceErrorNotAnInternalError(t *testing.T) {
	const partial = `{"type":"partial_regenerate","time_ranges":["1.0-2.0"],"changes":["camera"],"instruction":"Zoom in."}`
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.llm.interpreter = opsJSON(partial)

	_, err := f.svc.Director.DirectorCommand(context.Background(), f.shotID, "最初の1秒だけ寄って")
	require.ErrorIs(t, err, service.ErrUnavailable)
	f.startDirecting(t)
	_, err = f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "最初の1秒だけ寄って"})
	require.ErrorIs(t, err, service.ErrUnavailable)

	var notices []string
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindText && m.Role == domain.RoleAssistant && strings.Contains(m.Content, "指示を実行できませんでした") {
			notices = append(notices, m.Content)
		}
	}
	require.Len(t, notices, 2)
	for _, n := range notices {
		assert.Contains(t, n, "機能が有効になっていません")
		assert.NotContains(t, n, "内部エラー")
	}
}

// A failure of the repository while the Command Interpreter's input is gathered (not a failure of
// the model) is an error of the message, as DirectorCommand returns it; it is not reported as a
// successful assistant message that "the direction could not be read". An error of the model is.
func TestChat_DirectorModeARepositoryFailureOfInterpretIsAnErrorNotAMessage(t *testing.T) {
	f := newGenFixture(t, true)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"unsupported","reason":"x"}`)
	f.execSQL(t, `ALTER TABLE lock RENAME TO lock_gone`)

	_, err := f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "カメラを寄せて"})

	require.Error(t, err)
	assert.NotErrorIs(t, err, service.ErrUnavailable)
	ws, err := f.svc.Chat.Workspace(context.Background(), f.projectID)
	require.NoError(t, err)
	last := lastMessage(t, ws.Messages)
	assert.Equal(t, domain.RoleAssistant, last.Role)
	assert.Contains(t, last.Content, "指示を実行できませんでした", "the thread does not end with the user's words alone")
	assert.NotContains(t, last.Content, "指示を読み取れませんでした")
}

func TestChat_DirectorModeAFailedModelCallIsAMessage(t *testing.T) {
	f := newGenFixture(t, true)
	f.startDirecting(t)
	f.llm.interpreterErr = fmt.Errorf("interpret: %w", llm.ErrCircuitOpen)

	ws := f.say(t, "カメラを寄せて", "")

	assert.Contains(t, lastMessage(t, ws.Messages).Content, "指示を読み取れませんでした: AI サービスが一時的に利用できません")
}

func TestDirectorCommand_PinsTheVideoModelToRunwayAndAsksForAnyModelWithoutAName(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"video_model","mode":"manual","value":"runway-gen-4.5"}`)

	res, err := f.svc.Director.DirectorCommand(context.Background(), f.shotID, "この Shot の動画モデルを Runway に固定して")

	require.NoError(t, err)
	assert.Equal(t, domain.LockManual, res.Inspector.Locks[domain.ParamVideoModel].Mode)
	assert.Equal(t, "runway-gen-4.5", res.Inspector.Locks.Text(domain.ParamVideoModel))

	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"video_model","mode":"manual"}`)
	_, err = f.svc.Director.DirectorCommand(context.Background(), f.shotID, "動画モデルは手動にして")
	require.NoError(t, err)
	ws, err := f.svc.Chat.Workspace(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Contains(t, lastMessage(t, ws.Messages).Content, "kling-3.0 / seedance-2.0 / runway-gen-4.5")
}

func (f genFixture) startDirecting(t *testing.T) {
	t.Helper()
	f.execSQL(t, `UPDATE project SET mode = 'director', state = 'GENERATING' WHERE id = ?`, f.projectID)
}

func (f genFixture) say(t *testing.T, content, replyTo string) service.Workspace {
	t.Helper()
	return postMessage(t, f.svc, f.projectID, service.PostMessageInput{Content: content, ReplyToMessageID: replyTo})
}

func postMessage(t *testing.T, svc *service.Services, projectID string, in service.PostMessageInput) service.Workspace {
	t.Helper()
	ws, err := svc.Chat.PostMessage(context.Background(), projectID, in)
	require.NoError(t, err)
	return ws
}

func TestChat_DirectorModeDirectionAttachmentsJoinTheBrief(t *testing.T) {
	f := newGenFixture(t, true)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"unsupported","reason":"書き出しはまだ指示できません"}`)
	before, err := f.svc.Projects.Brief(context.Background(), f.projectID)
	require.NoError(t, err)

	postMessage(t, f.svc, f.projectID, service.PostMessageInput{Content: "この画像の雰囲気で", Uploads: []service.Upload{upload("look.png", pngBytes)}})

	after, err := f.svc.Projects.Brief(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Len(t, after.Brief.Assets, len(before.Brief.Assets)+1)
}

func TestChat_DirectorModeResolvesTheShotFromAReplyToATakeCard(t *testing.T) {
	f := newGenFixture(t, true)
	second, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"})
	require.NoError(t, err)
	f.generateAndWait(t, second.Shots[1].ID, 1)
	f.startDirecting(t)
	var card domain.ThreadMessage
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindTake {
			card = m
		}
	}
	require.NotEmpty(t, card.ID)
	f.llm.interpreter = opsJSON(`{"type":"regenerate","changes":["acting"],"instruction":"Make it more nervous."}`)

	ws := f.say(t, "この瞬間だけ不安そうにして", card.ID)

	require.Len(t, ws.Messages, 2)
	assert.Contains(t, ws.Messages[1].Content, "Shot 2 だけを再生成")
	last := f.llm.prompts(llm.AgentCommandInterpreter)
	assert.Contains(t, last[len(last)-1], "about Scene 1 / Shot 2", "the reply target resolved the Shot")
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'succeeded' AND kind = 'generate'`) == 2
	}, 30*time.Second, 10*time.Millisecond)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, f.shotID))

	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"camera","mode":"lock"}`)
	ws = f.say(t, "カメラは固定", "")
	assert.Contains(t, ws.Messages[1].Content, "Shot 2", "without a reply the latest Take card gives the context")
	locks, err := f.svc.Director.Inspector(context.Background(), second.Shots[1].ID)
	require.NoError(t, err)
	assert.True(t, locks.Locks.Locked(domain.ParamCamera))
}

func TestChat_ReplyToATakeCardIgnoresSceneShotNumbersInTheMessage(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	var shot1Card domain.ThreadMessage
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindTake {
			shot1Card = m
		}
	}
	require.NotEmpty(t, shot1Card.ID)

	second, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"})
	require.NoError(t, err)
	shot2ID := second.Shots[1].ID
	f.generateAndWait(t, shot2ID, 2)
	f.startDirecting(t)
	f.llm.interpreter = func(string) string {
		return `{"scene":1,"shot":2,"operations":[{"type":"set_lock","parameter":"camera","mode":"lock"}]}`
	}

	ws := f.say(t, "Scene 1 の Shot 2 のカメラを固定して", shot1Card.ID)

	assert.Contains(t, ws.Messages[1].Content, "Scene 1 / Shot 1", "the Take card being replied to wins over numbers in the message")
	locks1, err := f.svc.Director.Inspector(context.Background(), f.shotID)
	require.NoError(t, err)
	assert.True(t, locks1.Locks.Locked(domain.ParamCamera))
	locks2, err := f.svc.Director.Inspector(context.Background(), shot2ID)
	require.NoError(t, err)
	assert.False(t, locks2.Locks.Locked(domain.ParamCamera))
}

func TestChat_DirectorModeAsksWhenTheShotCannotBeResolved(t *testing.T) {
	f := newGenFixture(t, true)
	_, err := f.svc.Plans.CreateShot(context.Background(), f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"})
	require.NoError(t, err)
	f.startDirecting(t)

	f.llm.interpreter = opsJSON(`{"type":"regenerate","changes":["camera"],"instruction":"Zoom in."}`)
	ws := f.say(t, "もう少し寄って", "")
	assert.Contains(t, ws.Messages[1].Content, "どの Shot への指示ですか", "no target: the AI asks instead of guessing")
	assert.Zero(t, f.lockRows(t))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job`))

	f.llm.interpreter = func(string) string { return `{"question":"どの Shot ですか？","operations":[]}` }
	ws = f.say(t, "カメラ動かしすぎ", "")
	assert.Equal(t, "どの Shot ですか？", ws.Messages[1].Content, "the Interpreter's own question is passed on")

	f.llm.interpreter = func(string) string {
		return `{"scene":1,"shot":2,"operations":[{"type":"set_lock","parameter":"camera","mode":"lock"}]}`
	}
	ws = f.say(t, "Scene 1 の Shot 2 のカメラを固定して", "")
	assert.Contains(t, ws.Messages[1].Content, "Scene 1 / Shot 2", "the Shot named in words is used")
	assert.Equal(t, 1, f.lockRows(t))
}

func TestChat_Example4RepliesWithTheFixedSentenceAndCreatesNothing(t *testing.T) {
	f := newGenFixture(t, true)
	f.startDirecting(t)
	shots := f.count(t, `SELECT COUNT(*) FROM shot`)
	timelines := f.count(t, `SELECT COUNT(*) FROM edit_timeline_version`)
	jobs := f.count(t, `SELECT COUNT(*) FROM job`)

	f.llm.interpreter = opsJSON(`{"type":"unsupported","reason":"尺はタイムライン側で調整してください"}`)
	ws := f.say(t, "商品をあと1秒長く見せて", "")
	assert.Equal(t, "その操作にはまだ対応していません", ws.Messages[1].Content)

	f.llm.interpreter = opsJSON(`{"type":"unsupported","reason":"追加のカットはまだチャットから作れません"}`)
	ws = f.say(t, "このあとにカットを1本足して", "")
	assert.Equal(t, "その操作にはまだ対応していません", ws.Messages[1].Content)

	assert.Equal(t, shots, f.count(t, `SELECT COUNT(*) FROM shot`))
	assert.Equal(t, timelines, f.count(t, `SELECT COUNT(*) FROM edit_timeline_version`))
	assert.Equal(t, jobs, f.count(t, `SELECT COUNT(*) FROM job`))
}

func TestChat_DirectorModeCanSwitchBackToAutoAndReportsWhatIsNotSupported(t *testing.T) {
	f := newGenFixture(t, true)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"unsupported","reason":"書き出しはまだ指示できません"}`)
	ws := f.say(t, "これを書き出して", "")
	assert.Contains(t, ws.Messages[1].Content, "書き出し")

	f.llm.interpreter = opsJSON(`{"type":"set_mode","project_mode":"auto"}`)
	ws = f.say(t, "あとはお任せ", "")
	p, err := f.svc.Projects.Get(context.Background(), f.projectID)
	require.NoError(t, err)
	assert.Equal(t, domain.ModeAuto, p.Mode)
	var thread []string
	for _, m := range ws.Messages {
		thread = append(thread, m.Content)
	}
	all := strings.Join(thread, "\n")
	assert.Equal(t, 1, strings.Count(all, "AUTO MODE に戻しました"), "the switch is told once")
	assert.NotContains(t, all, "見つかりませんでした", "a switch alone is an executable direction")

	ws = f.say(t, "こんにちは", "")
	assert.Len(t, ws.Messages, 1, "in AUTO the message is not a direction")
}

func TestChat_AutoModeMessagesAreNotInterpreted(t *testing.T) {
	f := newGenFixture(t, true)
	before := f.llm.calls(llm.AgentCommandInterpreter)
	f.say(t, "カメラを固定して", "")
	assert.Equal(t, before, f.llm.calls(llm.AgentCommandInterpreter))
}

func TestProjectLock_LockedStoryIsNotChangedByTheStateExtractor(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.buildReferencePack(t)
	_, err := f.svc.Director.SetProjectLock(context.Background(), f.projectID, domain.ParamStory, domain.LockLocked)
	require.NoError(t, err)

	f.generateAndWait(t, f.shotID, 2)

	world, err := f.svc.World.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	assert.Empty(t, world.World.Story.PreviousEvent, "the Story is frozen")
	assert.Zero(t, world.World.Story.Scene)
	assert.Equal(t, "hallway_door", world.World.Characters[0].Position, "the rest of the World State still follows the Shot")
	assert.True(t, world.ProjectLocks.Locked(domain.ParamStory))
	assert.False(t, world.ProjectLocks.Locked(domain.ParamMusic))
}

func TestInspector_ShowsEveryParameterSetting(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.setLock(t, domain.ParamCamera, domain.LockLocked)

	view, err := f.svc.Director.Inspector(context.Background(), f.shotID)

	require.NoError(t, err)
	require.NotNil(t, view.Spec)
	assert.Len(t, view.Takes, 3)
	assert.Equal(t, domain.LockLocked, view.Locks.Mode(domain.ParamCamera))
	assert.Equal(t, domain.LockAuto, view.Locks.Mode(domain.ParamActing), "a parameter without a row is AUTO")
	assert.Contains(t, view.SpecText(), `"shot_objective"`)
	_, err = f.svc.Director.Inspector(context.Background(), "nope")
	assert.ErrorIs(t, err, service.ErrNotFound)
}

// A direction of the chat that fails after part of it was done (a LOCK was set, then the planned
// spec broke a LOCK Brand constraint) is answered in the thread with what was and was not done, as
// the director-command path does: the thread does not end with the user's words alone.
func TestChat_DirectorModeAFailedDirectionIsAnsweredWithWhatWasDone(t *testing.T) {
	f := newGenFixture(t, true)
	f.firstGeneration(t)
	f.startDirecting(t)
	_, err := f.svc.Brand.Add(context.Background(), f.projectID, service.BrandForm{Type: string(domain.BrandForbidden), Text: "BANNED"})
	require.NoError(t, err)
	f.llm.planner = func(string) string {
		return strings.Replace(plannerRewrite, "Character notices something behind her", "a BANNED word notices something", 1)
	}
	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"camera","mode":"lock"}`,
		`{"type":"regenerate","changes":["acting"],"instruction":"Make her nervous."}`)

	_, err = f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "カメラを固定して、もっと不安そうに"})
	require.ErrorIs(t, err, domain.ErrBrandViolation)

	ws, err := f.svc.Chat.Workspace(context.Background(), f.projectID)
	require.NoError(t, err)
	last := lastMessage(t, ws.Messages)
	assert.Equal(t, domain.RoleAssistant, last.Role)
	assert.Contains(t, last.Content, "指示を実行できませんでした")
	assert.Contains(t, last.Content, "Brand / Safety Constraint")
	assert.Contains(t, last.Content, "Camera を LOCK にしました", "what was done before the failure is told")
}

// A reply to a Keyframe card is a direction about the Shot whose Reference Pack holds that image,
// as a reply to a Take card is, not about the Shot of the latest Take card; and a Keyframe whose
// Shot is not known is asked about instead of being applied to another Shot.
func TestChat_DirectorModeResolvesTheShotFromAReplyToAKeyframeCard(t *testing.T) {
	ctx := context.Background()
	f := newGenFixture(t, true)
	f.firstGeneration(t) // Shot 1 has the latest Take card
	second, err := f.svc.Plans.CreateShot(ctx, f.sceneID, service.ShotForm{Objective: "ドアを開ける", DurationSeconds: "4"})
	require.NoError(t, err)
	shot2ID := second.Shots[1].ID
	f.execSQL(t, `UPDATE shot SET shot_spec = json_set(shot_spec, '$.references', json('[{"kind":"keyframe","asset_id":"kf-2","prompt":"p","model":"m"}]')) WHERE id = ?`, shot2ID)
	require.NoError(t, f.svc.Chat.Announce(ctx, f.projectID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindKeyframe, RefID: "kf-2", Content: "Shot 2: Keyframe"}))
	require.NoError(t, f.svc.Chat.Announce(ctx, f.projectID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindKeyframe, RefID: "kf-unknown", Content: "Shot ?: Keyframe"}))
	var known, unknown domain.ThreadMessage
	for _, m := range f.thread(t, f.projectID) {
		if m.Kind == domain.KindKeyframe && m.RefID == "kf-2" {
			known = m
		}
		if m.Kind == domain.KindKeyframe && m.RefID == "kf-unknown" {
			unknown = m
		}
	}
	require.NotEmpty(t, known.ID)
	f.startDirecting(t)
	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"camera","mode":"lock"}`)

	ws := f.say(t, "カメラは固定", known.ID)

	assert.Contains(t, ws.Messages[len(ws.Messages)-1].Content, "Shot 2")
	locks2, err := f.svc.Director.Inspector(ctx, shot2ID)
	require.NoError(t, err)
	assert.True(t, locks2.Locks.Locked(domain.ParamCamera), "the Keyframe's Shot is the target")
	locks1, err := f.svc.Director.Inspector(ctx, f.shotID)
	require.NoError(t, err)
	assert.False(t, locks1.Locks.Locked(domain.ParamCamera))

	f.llm.interpreter = opsJSON(`{"type":"set_lock","parameter":"lighting","mode":"lock"}`)
	f.say(t, "照明は固定", unknown.ID)
	locks1, err = f.svc.Director.Inspector(ctx, f.shotID)
	require.NoError(t, err)
	assert.False(t, locks1.Locks.Locked(domain.ParamLighting), "no other Shot is directed when the Keyframe's Shot is unknown")
	locks2, err = f.svc.Director.Inspector(ctx, shot2ID)
	require.NoError(t, err)
	assert.False(t, locks2.Locks.Locked(domain.ParamLighting))
}

// A message sent without a reply is about the latest Take card's Shot. When that Shot is on a Branch the
// Scene/Shot numbers are those of the Branch's story: the trunk Shot that has the same numbers (a Branch
// Scene counts from 1 again) is not the one to direct.
func TestChat_DirectorModeANumberedShotOfABranchIsNotTheTrunkShotWithTheSameNumbers(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	ctx := context.Background()
	d := f.branch(t, service.BranchInput{Direction: "彼女がドアを開ける"})
	require.Len(t, d.Shots, 1)
	branchShotID := d.Shots[0].Shot.ID
	f.waitGenerateJobs(t, 3)
	f.startDirecting(t)
	scene, shot := d.Scenes[0].OrderIndex+1, d.Shots[0].Shot.OrderIndex+1
	f.llm.interpreter = func(string) string {
		return fmt.Sprintf(`{"scene":%d,"shot":%d,"operations":[{"type":"set_lock","parameter":"camera","mode":"lock"}]}`, scene, shot)
	}

	f.say(t, "カメラは固定して", "")

	inBranch, err := f.svc.Director.Inspector(ctx, branchShotID)
	require.NoError(t, err)
	assert.True(t, inBranch.Locks.Locked(domain.ParamCamera), "the Shot of the latest Take card is the one directed")
	onTrunk, err := f.svc.Director.Inspector(ctx, f.shotID)
	require.NoError(t, err)
	assert.False(t, onTrunk.Locks.Locked(domain.ParamCamera), "the trunk Shot with the same numbers is left alone")
}

// switch_to_director counts the switches AUTO -> DIRECTOR. Asking for DIRECTOR MODE while in it switches
// nothing: it is neither recorded nor told as a switch (spec/spec.md §28.3).
func TestChat_AskingForTheModeTheProjectIsAlreadyInRecordsNoSwitch(t *testing.T) {
	f := newGenFixture(t, true)
	f.startDirecting(t)
	switches := func() int {
		return f.count(t, `SELECT COUNT(*) FROM human_intervention WHERE project_id = ? AND kind = 'switch_to_director'`, f.projectID)
	}

	f.llm.interpreter = opsJSON(`{"type":"set_mode","project_mode":"director"}`)
	ws := f.say(t, "DIRECTOR にして", "")

	assert.Zero(t, switches())
	assert.Contains(t, ws.Messages[len(ws.Messages)-1].Content, "すでに DIRECTOR MODE です")
	assert.NotContains(t, ws.Messages[len(ws.Messages)-1].Content, "にしました")

	f.llm.interpreter = opsJSON(`{"type":"set_mode","project_mode":"auto"}`, `{"type":"set_mode","project_mode":"director"}`)
	f.say(t, "AUTO にして、やっぱり DIRECTOR", "")
	assert.Equal(t, 1, switches(), "a real switch back to DIRECTOR is recorded")
}
