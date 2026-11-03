package planadvice_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/planadvice"
)

const noKeyReason = "LLM の API キーが設定されていません。失敗した Agent は Producer（コンセプト）です。"

func plan(status domain.JobStatus, reason string) domain.Job {
	return domain.Job{Kind: domain.JobPlan, Status: status, Error: reason}
}

func TestFor_TellsHowToGoOnWhenThePlanStoppedForWantOfAKey(t *testing.T) {
	screen := planadvice.For(plan(domain.JobFailed, noKeyReason), false)
	assert.Contains(t, screen, "登場人物を追加", "the screen names its button")
	assert.Contains(t, screen, "API キーとログイン")
	assert.NotContains(t, screen, "create_character")

	mcp := planadvice.For(plan(domain.JobFailed, noKeyReason), true)
	assert.Contains(t, mcp, "create_character", "the MCP names its tools")
	assert.Contains(t, mcp, "set_credential")
	assert.Contains(t, mcp, "post_chat_message")
	assert.NotContains(t, mcp, "登場人物を追加")

	assert.NotEmpty(t, planadvice.For(plan(domain.JobFailed, "API キーが設定されていません（設定画面で…）"), true), "the short wording of a failure without a route counts too")
}

func TestFor_IsEmptyForAnyOtherJobOrFailure(t *testing.T) {
	for name, j := range map[string]domain.Job{
		"another failure":  plan(domain.JobFailed, "AI の応答がタイムアウトしました"),
		"no failure":       plan(domain.JobFailed, ""),
		"still running":    plan(domain.JobRunning, noKeyReason),
		"another kind":     {Kind: domain.JobGenerate, Status: domain.JobFailed, Error: noKeyReason},
		"internal failure": plan(domain.JobFailed, "内部エラーが発生しました"),
	} {
		assert.Empty(t, planadvice.For(j, true), name)
	}
}

func TestFor_SaysWhatToDoWithTheDraftsWhenTheReasonTellsThem(t *testing.T) {
	drafted := plan(domain.JobFailed, noKeyReason+"\n"+planadvice.DraftNote(2))

	screen := planadvice.For(drafted, false)
	assert.Contains(t, screen, "下書き", "the screen names the mark")
	assert.Contains(t, screen, "API キーとログイン", "the way with an LLM is still told")
	assert.Contains(t, screen, "チャットに何かメッセージを送ると")
	assert.NotContains(t, screen, "update_character")

	mcp := planadvice.For(drafted, true)
	assert.Contains(t, mcp, "get_world_state", "the MCP names where to see the drafts")
	assert.Contains(t, mcp, "draft")
	assert.Contains(t, mcp, "update_character")
	assert.Contains(t, mcp, "post_chat_message")
	assert.NotContains(t, mcp, "下書きの印")

	assert.Contains(t, planadvice.DraftNote(3), "3 件", "the note carries the number")
	assert.NotContains(t, planadvice.For(plan(domain.JobFailed, noKeyReason), true), "draft", "no draft: the plain advice")
}
