package service_test

import (
	"context"
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
