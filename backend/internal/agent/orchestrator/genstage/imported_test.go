package genstage_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
)

// A bundle carries the chat cards of a Project but not generation_approval (the approval is of this instance's models and prices). An approved
// card with no record must not stand for the plan, even when the plan is the very one the card was posted for (same fingerprint): nothing is
// started, the approval is asked for again with a new card, and the Project waits for it.
func TestGenerationApproval_AnApprovedCardWithoutARecordIsNeverTheApproval(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.approved(domain.ConfirmFullAuto)
	ctx := context.Background()
	approvedCard := r.card(id)
	require.NotNil(t, approvedCard)
	require.Equal(t, domain.ApprovalApproved, approvedCard.Status)
	require.NoError(t, r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM generation_approval WHERE message_id = ?`, approvedCard.ID)
		return err
	}), "the record the bundle does not carry")

	phase, _, err := r.gate.Phase(ctx, r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseApproval, phase, "the screen waits for an approval, it does not say the generation goes on by itself")

	r.handle(id)
	r.handle(id)

	assert.Empty(t, r.gen.started(), "no Reference Pack, Keyframe or Video on a card that has no record")
	assert.Equal(t, 2, r.cards(id), "the approval is asked for again, once")
	assert.Equal(t, domain.ApprovalPending, r.card(id).Status)
	assert.NotEqual(t, approvedCard.ID, r.card(id).ID)
	assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)
}
