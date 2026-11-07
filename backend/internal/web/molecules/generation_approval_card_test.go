package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// The generation approval card (spec/spec.md §4.3) is found by its data-testid. It shows what will be made and the estimate ("不明" when a price
// is not known, never a made-up number), and its buttons approve or send it back.
func TestApprovalCard_GenerationApprovalShowsItsContentEstimateAndBothAnswers(t *testing.T) {
	body := "生成の承認: 次の内容で、Reference Pack → Shot 生成を自動で進めます。\n・対象: Shot 3 件（Shot 1・2・3）\n・費用の見積: 不明（単価が分かりません）（Reference Pack 不明、動画 最大 6.7500 USD）"
	html := render(t, molecules.ApprovalCard(domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: "a1", ProjectID: "p1", Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: body,
		RefType: domain.CheckpointGeneration.RefType(), RefID: "fp", ApprovalStatus: domain.ApprovalPending,
	}}))

	assert.Contains(t, html, `data-testid="generation-approval-card"`)
	assert.Contains(t, html, `data-approval-status="pending"`)
	assert.Contains(t, html, `data-testid="approval-summary"`)
	assert.Contains(t, html, "Shot 3 件（Shot 1・2・3）")
	assert.Contains(t, html, "費用の見積: 不明")
	assert.Contains(t, html, "確認: 生成する内容と費用")
	assert.Contains(t, html, `data-testid="approval-approve"`)
	assert.Contains(t, html, `data-testid="approval-request_changes"`)
	assert.Contains(t, html, "確認待ち")
}

// Once answered the card stays in the thread with its answer and no buttons.
func TestApprovalCard_AnsweredGenerationApprovalHasNoButtons(t *testing.T) {
	for status, badge := range map[domain.ApprovalStatus]string{domain.ApprovalApproved: "承認済み", domain.ApprovalChangesRequested: "修正依頼済み"} {
		html := render(t, molecules.ApprovalCard(domain.ThreadMessage{ChatMessage: domain.ChatMessage{
			ID: "a1", ProjectID: "p1", Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "x",
			RefType: domain.CheckpointGeneration.RefType(), ApprovalStatus: status,
		}}))
		assert.Contains(t, html, badge)
		assert.Contains(t, html, `data-approval-status="`+string(status)+`"`)
		assert.NotContains(t, html, `data-testid="approval-approve"`)
	}
}

// The other approval cards keep their own test id.
func TestApprovalCard_ScriptApprovalIsNotTheGenerationApproval(t *testing.T) {
	html := render(t, molecules.ApprovalCard(domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: "a1", ProjectID: "p1", Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "x",
		RefType: domain.CheckpointScript.RefType(), ApprovalStatus: domain.ApprovalPending,
	}}))
	assert.Contains(t, html, `data-testid="approval-card"`)
	assert.NotContains(t, html, "generation-approval-card")
}
