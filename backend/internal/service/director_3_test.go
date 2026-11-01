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
	f.waitCount(t, 30*time.Second, 2, `SELECT COUNT(*) FROM job WHERE status = 'succeeded' AND kind = 'generate'`)
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
