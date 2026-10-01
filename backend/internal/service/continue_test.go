package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

const (
	proposeAnswer = `{"analysis":{"final_frame":"ドアの前で振り返る主人公","story":"主人公が部屋に戻った","character":"黒いコート","position":"玄関ドア前",
		"emotion":"不安","object":"","camera":"ミディアムで静止","lighting":"冷たい青","motion":"停止","dialogue":"「誰？」","music":"低いドローン"},
		"candidates":["彼女がドアを開ける","背後から声が聞こえる","突然照明が消える"]}`
	sameSceneAnswer = `{"new_scene":false,"shot":{"objective":"ドアを開けて廊下を見る","duration_seconds":4}}`
	newSceneAnswer  = `{"new_scene":true,"scene":{"title":"廊下","location":"マンションの廊下","time":"夜","objective":"追ってくる相手を見つける"},
		"shot":{"objective":"廊下の奥に人影が立っている","duration_seconds":90}}`
)

// directorFor answers Continue: the candidates for the proposal prompt, plan for the plan prompt.
func directorFor(plan string) func(system, user string) string {
	return func(system, _ string) string {
		if strings.Contains(system, `"candidates"`) {
			return proposeAnswer
		}
		return plan
	}
}

// completedShotFixture is a Project whose first Shot has Takes and a World State.
func completedShotFixture(t *testing.T, plan string) genFixture {
	t.Helper()
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.llm.director = directorFor(plan)
	f.buildReferencePack(t)
	f.generateAndWait(t, f.shotID, 2)
	return f
}

func (f genFixture) propose(t *testing.T) domain.ThreadMessage {
	t.Helper()
	_, card, err := f.svc.Continue.Propose(context.Background(), f.shotID)
	require.NoError(t, err)
	return card
}

// waitGenerateJobs waits until n generate jobs (Reference Pack included) have succeeded.
func (f genFixture) waitGenerateJobs(t *testing.T, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM job WHERE status = 'succeeded' AND kind = 'generate'`) == n
	}, 30*time.Second, 10*time.Millisecond, "generate jobs did not finish")
}

func (f genFixture) shotObjectives(t *testing.T) []string {
	t.Helper()
	rows, err := f.db.Reader().QueryContext(context.Background(),
		`SELECT sh.objective FROM shot sh JOIN scene s ON s.id = sh.scene_id ORDER BY s.order_index, sh.order_index`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var o string
		require.NoError(t, rows.Scan(&o))
		out = append(out, o)
	}
	return out
}

func TestContinuePropose_PostsAQuestionCardWithTheAnalysisAndNextCandidates(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)

	card := f.propose(t)

	assert.Equal(t, domain.KindQuestion, card.Kind)
	assert.Equal(t, domain.RefContinue, card.RefType)
	assert.Equal(t, f.shotID, card.RefID)
	assert.True(t, card.Open)
	body := domain.DecodeCardBody(card.Content)
	assert.Equal(t, []string{"A. 彼女がドアを開ける", "B. 背後から声が聞こえる", "C. 突然照明が消える"}, body.Options)
	for _, want := range []string{"最終フレーム: ドアの前で振り返る主人公", "位置: 玄関ドア前", "感情: 不安", "セリフ: 「誰？」", "音楽: 低いドローン", "D は入力欄", "E は「AIに任せる」"} {
		assert.Contains(t, body.Text, want)
	}
	assert.NotContains(t, body.Text, "オブジェクト", "a State the Shot does not show is left out")

	// The analysis reads the World State the completed Shot left (F-16).
	prompts := f.llm.users[llm.AgentDirector]
	require.Len(t, prompts, 1)
	assert.Contains(t, prompts[0], `emotion="anxious"`)
	assert.Contains(t, prompts[0], "phone_call_ended")
	assert.Contains(t, prompts[0], "Scene 1 / Shot 1", "the story so far is part of the analysis input")
	last := f.thread(t, f.projectID)
	assert.Equal(t, card.ID, last[len(last)-1].ID, "the card is in the thread")
}

func TestContinuePropose_NeedsACompletedShotAndAConfiguredGeneration(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.director = directorFor(sameSceneAnswer)
	_, _, err := f.svc.Continue.Propose(context.Background(), f.shotID)
	assert.ErrorIs(t, err, service.ErrConflict, "no Take yet")
	_, _, err = f.svc.Continue.Propose(context.Background(), "missing")
	assert.ErrorIs(t, err, service.ErrNotFound)

	f = newGenFixture(t, false)
	_, _, err = f.svc.Continue.Propose(context.Background(), f.shotID)
	assert.ErrorIs(t, err, service.ErrUnavailable)
}

func TestContinuePropose_UnusableAIAnswerIsAnAIErrorAndPostsNothing(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	f.llm.director = func(string, string) string { return `{"analysis":{},"candidates":[]}` }
	before := len(f.thread(t, f.projectID))

	_, _, err := f.svc.Continue.Propose(context.Background(), f.shotID)

	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Len(t, f.thread(t, f.projectID), before)
}

func TestContinue_ChoosingACandidateAddsTheNextShotToTheSceneAndGeneratesItFromTheWorldState(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	card := f.propose(t)

	ws, err := f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "B. 背後から声が聞こえる", ReplyToMessageID: card.ID})
	require.NoError(t, err)
	require.NotEmpty(t, ws.Messages)

	// The next Shot is linked into the existing Scene, after the completed one.
	assert.Equal(t, []string{"主人公が振り返って走り出す", "ドアを開けて廊下を見る"}, f.shotObjectives(t))
	f.waitGenerateJobs(t, 3)
	// The chosen candidate (without its letter) and the World State reach the planners.
	plan := f.llm.users[llm.AgentDirector][1]
	assert.Contains(t, plan, "背後から声が聞こえる")
	assert.NotContains(t, plan, "B. ")
	assert.Contains(t, plan, `position="hallway_door"`)
	planner := f.llm.users[llm.AgentPromptPlanner]
	require.Len(t, planner, 2)
	assert.Contains(t, planner[1], "背後から声が聞こえる", "the direction is the additional instruction of the new Shot")
	assert.Contains(t, planner[1], `emotion="anxious"`)
	// The card is closed.
	for _, m := range f.thread(t, f.projectID) {
		if m.ID == card.ID {
			assert.False(t, m.Open)
		}
	}
}

func TestContinue_LeavingItToTheAIOrWritingAFreeDirection(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	card := f.propose(t)
	_, err := f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: domain.DelegateOption, ReplyToMessageID: card.ID})
	require.NoError(t, err)
	f.waitGenerateJobs(t, 3)
	plan := f.llm.users[llm.AgentDirector][1]
	assert.Contains(t, plan, "AI に任せる", "the AI is told to choose by itself")
	assert.Len(t, f.shotObjectives(t), 2)

	free := completedShotFixture(t, sameSceneAnswer)
	card = free.propose(t)
	_, err = free.svc.Chat.PostMessage(context.Background(), free.projectID, service.PostMessageInput{Content: "  主人公が窓の外を見る ", ReplyToMessageID: card.ID})
	require.NoError(t, err)
	assert.Contains(t, free.llm.users[llm.AgentDirector][1], "主人公が窓の外を見る")
}

func TestContinueConfirm_TheStoryMovingOnCreatesANewSceneAndCapsTheDuration(t *testing.T) {
	f := completedShotFixture(t, newSceneAnswer)

	frag, err := f.svc.Continue.Confirm(context.Background(), f.shotID, service.ConfirmInput{Instruction: "誰かが追ってくる"})
	require.NoError(t, err)

	assert.Equal(t, domain.ShotGenerating, frag.State)
	assert.Equal(t, "廊下の奥に人影が立っている", frag.Objective)
	assert.Equal(t, 60.0, frag.Duration)
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM scene WHERE project_id = ?`, f.projectID))
	var title, loc string
	require.NoError(t, f.query(t, `SELECT title, (SELECT name FROM location WHERE id = location_id) FROM scene WHERE project_id = ? AND order_index = 1`, f.projectID).Scan(&title, &loc))
	assert.Equal(t, "廊下", title)
	assert.Equal(t, "マンションの廊下", loc)
	f.waitGenerateJobs(t, 3)
}

func TestContinueConfirm_RejectsMissingInputAndAnAlreadyAnsweredCard(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	_, err := f.svc.Continue.Confirm(context.Background(), f.shotID, service.ConfirmInput{Instruction: "  "})
	var verr *service.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, "instruction", verr.Field)
	_, err = f.svc.Continue.Confirm(context.Background(), f.shotID, service.ConfirmInput{Instruction: strings.Repeat("あ", 1001)})
	require.ErrorAs(t, err, &verr)
	assert.Len(t, f.shotObjectives(t), 1, "nothing was added")

	card := f.propose(t)
	_, err = f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "A. 彼女がドアを開ける", ReplyToMessageID: card.ID})
	require.NoError(t, err)
	f.waitGenerateJobs(t, 3)
	_, err = f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "C. 突然照明が消える", ReplyToMessageID: card.ID})
	assert.ErrorIs(t, err, service.ErrConflict)
	assert.Len(t, f.shotObjectives(t), 2)
}

func TestContinueConfirm_AnUnusablePlanLeavesTheProjectUnchanged(t *testing.T) {
	f := completedShotFixture(t, `{"new_scene":true,"scene":{"title":""},"shot":{"objective":"x"}}`)
	_, err := f.svc.Continue.Confirm(context.Background(), f.shotID, service.ConfirmInput{Delegate: true})
	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Len(t, f.shotObjectives(t), 1)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM scene WHERE project_id = ?`, f.projectID))
}

// A reply whose Confirm fails before the next Shot exists leaves the card open: the user answers
// the same card again, and only the answer that worked is recorded as its reply.
func TestContinueConfirm_AFailedReplyLeavesTheCardOpenForAnotherAnswer(t *testing.T) {
	f := completedShotFixture(t, `{"new_scene":true,"scene":{"title":""},"shot":{"objective":"x"}}`)
	card := f.propose(t)
	_, err := f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "A. 彼女がドアを開ける", ReplyToMessageID: card.ID})
	var ai *service.AIError
	require.ErrorAs(t, err, &ai)
	assert.Equal(t, 0, f.count(t, `SELECT COUNT(*) FROM chat_message WHERE reply_to_message_id = ?`, card.ID), "the failed answer is not a reply")
	assert.Len(t, f.shotObjectives(t), 1)

	f.llm.director = directorFor(sameSceneAnswer)
	ws, err := f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "B. 背後から声が聞こえる", ReplyToMessageID: card.ID})
	require.NoError(t, err)
	require.NotEmpty(t, ws.Messages)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM chat_message WHERE reply_to_message_id = ?`, card.ID))
	assert.Len(t, f.shotObjectives(t), 2)
	f.waitGenerateJobs(t, 3)
	_, err = f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "C. 突然照明が消える", ReplyToMessageID: card.ID})
	assert.ErrorIs(t, err, service.ErrConflict)
}
