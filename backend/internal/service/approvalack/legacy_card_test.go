package approvalack_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/approvalack"
)

// legacyGenerationCard puts a pending generation approval card of v0.1.34 into the thread (nothing posts one any more), holding the Project in
// AWAITING_APPROVAL. A card with a record has its generation_approval row; one without is what an imported bundle brings.
func legacyGenerationCard(t *testing.T, f fixture, projectID string, withRecord bool) domain.ChatMessage {
	t.Helper()
	ctx := context.Background()
	state := domain.StateAwaitingApproval
	card := domain.ChatMessage{
		ID: domain.NewID(), Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "生成の承認: 旧方針のカード",
		RefType: domain.CheckpointGeneration.RefType(), RefID: "fp", ApprovalStatus: domain.ApprovalPending,
	}
	require.NoError(t, repository.NewProjectRepository(f.db).Apply(ctx, repository.Turn{ProjectID: projectID, Messages: []domain.ChatMessage{card}, State: &state}))
	if withRecord {
		require.NoError(t, f.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO generation_approval (message_id, project_id, fingerprint, scope) VALUES (?, ?, 'fp', '[]')`, card.ID, projectID)
			return err
		}))
	}
	return card
}

// The answer to a generation card of v0.1.34 says what is true of that card: one with no record is not taken as an approval, in AUTO (what goes on is
// AUTO's own start) and outside it (nothing starts); the card says it has no record, and a card with its record is worded as before.
func TestRespondApproval_AnUnrecordedLegacyGenerationCardIsAnsweredAsNotBeingAnApproval(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		mode       domain.Mode
		withRecord bool
	}{{domain.ModeAuto, false}, {domain.ModeDirector, false}, {domain.ModeAuto, true}, {domain.ModeDirector, true}} {
		f := newFixture(t)
		p := f.createProject(t, "動画")
		_, err := f.svc.Director.SetMode(ctx, p.ID, tc.mode)
		require.NoError(t, err)
		card := legacyGenerationCard(t, f, p.ID, tc.withRecord)
		_, view, err := f.svc.Chat.Card(ctx, p.ID, card.ID)
		require.NoError(t, err)
		assert.Equal(t, !tc.withRecord, view.Unrecorded)

		project, updated, err := f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionApprove, "")

		require.NoError(t, err)
		assert.Equal(t, domain.StateVisualDesign, project.State, "the Project leaves AWAITING_APPROVAL")
		assert.Equal(t, domain.ApprovalApproved, updated.ApprovalStatus)
		assert.Equal(t, !tc.withRecord, updated.Unrecorded)
		assert.Equal(t, approvalack.Approved(card, tc.mode, !tc.withRecord), lastMessage(t, f.thread(t, p.ID)).Content, "%+v", tc)
	}
}

// A decline of a card with no record says that only a message lifts it (a change of the plan cannot, there is no record to compare with). The same
// answer in words, in a reply to the card, is worded the same, and a card with its record keeps its wording.
func TestRespondApproval_AnUnrecordedLegacyGenerationCardIsDeclinedWithTheRightGuidance(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "動画")
	card := legacyGenerationCard(t, f, p.ID, false)

	_, _, err := f.svc.Chat.RespondApproval(ctx, p.ID, card.ID, service.DecisionRequestChanges, "")

	require.NoError(t, err)
	assert.Equal(t, mainline.DeclinedAckUnrecorded, lastMessage(t, f.thread(t, p.ID)).Content)

	for withRecord, want := range map[bool]string{false: mainline.DeclinedAckUnrecorded, true: mainline.DeclinedAck} {
		q := f.createProject(t, "動画")
		reply := legacyGenerationCard(t, f, q.ID, withRecord)
		_, err := f.svc.Chat.PostMessage(ctx, q.ID, service.PostMessageInput{Content: "待ってください", ReplyToMessageID: reply.ID})
		require.NoError(t, err)
		assert.Equal(t, want, lastMessage(t, f.thread(t, q.ID)).Content, "record=%v", withRecord)
	}
}
