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
