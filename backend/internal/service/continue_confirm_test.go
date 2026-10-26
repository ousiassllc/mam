package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func TestContinueConfirm_TheStoryMovingOnCreatesANewSceneAndCapsTheDuration(t *testing.T) {
	f := completedShotFixture(t, newSceneAnswer)
	f.models.block = make(chan struct{})

	frag, err := f.svc.Continue.Confirm(context.Background(), f.shotID, service.ConfirmInput{Instruction: "誰かが追ってくる"})
	require.NoError(t, err)

	assert.Equal(t, domain.ShotGenerating, frag.State, "the Job is held: the answer cannot be read after it ended")
	close(f.models.block)
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
	postMessage(t, f.svc, f.projectID, service.PostMessageInput{Content: "A. 彼女がドアを開ける", ReplyToMessageID: card.ID})
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

// A direction with a forbidden expression of a LOCK Brand constraint is refused before the Plan is
// paid for and before a Shot is added; a reply to the card leaves the card open (spec §23).
func TestContinueConfirm_AForbiddenExpressionIsRefusedBeforeAnythingIsMade(t *testing.T) {
	f := completedShotFixture(t, sameSceneAnswer)
	ctx := context.Background()
	f.forbid(t, "BANNED")
	card := f.propose(t)
	planCalls := f.llm.calls(llm.AgentDirector)

	_, err := f.svc.Continue.Confirm(ctx, f.shotID, service.ConfirmInput{Instruction: "彼女が BANNED を持つ"})
	assert.ErrorIs(t, err, service.ErrBrandViolation)
	_, err = f.svc.Chat.PostMessage(ctx, f.projectID, service.PostMessageInput{Content: "BANNED な展開", ReplyToMessageID: card.ID})
	assert.ErrorIs(t, err, service.ErrBrandViolation)

	assert.Len(t, f.shotObjectives(t), 1, "no Shot is added")
	assert.Equal(t, planCalls, f.llm.calls(llm.AgentDirector), "the Plan is not called")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM chat_message WHERE reply_to_message_id = ?`, card.ID), "the card stays open")
	assert.NotContains(t, f.thread(t, f.projectID)[len(f.thread(t, f.projectID))-1].Content, "続きを追加しました")

	_, err = f.svc.Continue.Confirm(ctx, f.shotID, service.ConfirmInput{Instruction: "彼女がドアを開ける"})
	require.NoError(t, err, "a direction without the expression goes on")
	assert.Len(t, f.shotObjectives(t), 2)
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
	ws := postMessage(t, f.svc, f.projectID, service.PostMessageInput{Content: "B. 背後から声が聞こえる", ReplyToMessageID: card.ID})
	require.NotEmpty(t, ws.Messages)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM chat_message WHERE reply_to_message_id = ?`, card.ID))
	assert.Len(t, f.shotObjectives(t), 2)
	f.waitGenerateJobs(t, 3)
	_, err = f.svc.Chat.PostMessage(context.Background(), f.projectID, service.PostMessageInput{Content: "C. 突然照明が消える", ReplyToMessageID: card.ID})
	assert.ErrorIs(t, err, service.ErrConflict)
}
