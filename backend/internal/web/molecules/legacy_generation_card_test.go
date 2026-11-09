package molecules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// A generation approval card of v0.1.34 (#1108) that is still in a thread stays readable: the generic approval card draws its body and says it is of the
// old policy, and a pending one can still be answered (nothing posts a new one since #1115).
func TestApprovalCard_LegacyGenerationCardStaysReadableAndAnswerable(t *testing.T) {
	body := "生成の承認: 次の内容で、Reference Pack → Shot 生成を自動で進めます。\n・対象: Shot 3 件（Shot 1・2・3）"
	html := render(t, molecules.ApprovalCard(domain.ThreadMessage{ChatMessage: domain.ChatMessage{
		ID: "a1", ProjectID: "p1", Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: body,
		RefType: domain.CheckpointGeneration.RefType(), RefID: "fp", ApprovalStatus: domain.ApprovalPending,
	}}))

	assert.Contains(t, html, `data-testid="approval-card"`)
	assert.Contains(t, html, `data-approval-status="pending"`)
	assert.Contains(t, html, "Shot 3 件（Shot 1・2・3）")
	assert.Contains(t, html, "旧方針")
	assert.Contains(t, html, `data-testid="approval-approve"`)
	assert.Contains(t, html, `data-testid="approval-request_changes"`)
}

// Once answered the card stays in the thread with its answer and no buttons.
func TestApprovalCard_AnsweredLegacyGenerationCardHasNoButtons(t *testing.T) {
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

// A card with no record (it came with an imported bundle) says so, whatever its answer is: it is never an approval. A card with its record says nothing
// about it.
func TestApprovalCard_LegacyGenerationCardWithoutARecordSaysItIsNotAnApproval(t *testing.T) {
	for _, status := range []domain.ApprovalStatus{domain.ApprovalPending, domain.ApprovalApproved, domain.ApprovalChangesRequested} {
		card := domain.ThreadMessage{ChatMessage: domain.ChatMessage{
			ID: "a1", ProjectID: "p1", Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "x",
			RefType: domain.CheckpointGeneration.RefType(), ApprovalStatus: status,
		}}

		assert.NotContains(t, render(t, molecules.ApprovalCard(card)), `data-testid="approval-unrecorded"`, string(status))
		card.Unrecorded = true
		html := render(t, molecules.ApprovalCard(card))

		assert.Contains(t, html, `data-testid="approval-unrecorded"`, string(status))
		assert.Contains(t, html, "承認としては扱いません", string(status))
	}
}
