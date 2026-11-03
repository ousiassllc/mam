package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/planadvice"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func progress(job domain.Job) domain.ThreadMessage {
	return domain.ThreadMessage{
		ChatMessage: domain.ChatMessage{Kind: domain.KindProgress, RefType: domain.RefJob, RefID: job.ID, Content: "企画を作成中"},
		Job:         &job,
	}
}

// A planning job that stopped for want of an LLM key says on its card how to go on (#1001), under the failure text, which is not rewritten.
func TestProgressCard_AFailedPlanForWantOfAKeyHasNextSteps(t *testing.T) {
	const reason = "LLM の API キーが設定されていません。失敗した Agent は Producer（コンセプト）です。"
	html := render(t, molecules.ProgressCard(progress(domain.Job{ID: "j1", Kind: domain.JobPlan, Status: domain.JobFailed, Error: reason})))

	assert.Contains(t, html, reason)
	assert.Contains(t, html, `data-testid="progress-next-steps"`)
	assert.Contains(t, html, "登場人物を追加")
	assert.Contains(t, html, "API キーとログイン")
}

// A plan that stopped after drafting Characters from the attached images tells it, and says what to do with the drafts (#1022).
func TestProgressCard_ADraftedPlanFailureTellsWhatToDoWithTheDrafts(t *testing.T) {
	reason := "LLM の API キーが設定されていません。\n" + planadvice.DraftNote(2)
	html := render(t, molecules.ProgressCard(progress(domain.Job{ID: "j1", Kind: domain.JobPlan, Status: domain.JobFailed, Error: reason})))

	assert.Contains(t, html, "登場人物の下書き", "the card says the drafts were made")
	assert.Contains(t, html, `data-testid="progress-next-steps"`)
	assert.Contains(t, html, "「下書き」の印")
	assert.Contains(t, html, "チャットに何かメッセージを送ると", "and the way with an LLM")
}

func TestProgressCard_OtherJobsAndFailuresHaveNoNextSteps(t *testing.T) {
	for name, job := range map[string]domain.Job{
		"another failure": {ID: "j1", Kind: domain.JobPlan, Status: domain.JobFailed, Error: "AI の応答がタイムアウトしました"},
		"still running":   {ID: "j2", Kind: domain.JobPlan, Status: domain.JobRunning},
		"another kind":    {ID: "j3", Kind: domain.JobGenerate, Status: domain.JobFailed, Error: "生成モデルの API キーが設定されていません"},
	} {
		assert.NotContains(t, render(t, molecules.ProgressCard(progress(job))), "progress-next-steps", name)
	}
}
