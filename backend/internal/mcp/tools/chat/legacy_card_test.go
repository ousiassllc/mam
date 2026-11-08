package chat_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/chat"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// legacyGenerationCard is the card v0.1.34 left in a thread: a pending generation approval that holds the Project in AWAITING_APPROVAL (nothing posts
// one any more, so the row is made directly). It has no record (generation_approval), like the card of an imported bundle; withRecord adds one.
func legacyGenerationCard(t *testing.T, env *mcptest.Env, p domain.Project, withRecord ...bool) string {
	t.Helper()
	state := domain.StateAwaitingApproval
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "生成の承認: 旧方針のカード",
		RefType: domain.CheckpointGeneration.RefType(), RefID: "fp1", ApprovalStatus: domain.ApprovalPending,
	}
	require.NoError(t, repository.NewProjectRepository(env.DB).Apply(context.Background(), repository.Turn{ProjectID: p.ID, Messages: []domain.ChatMessage{card}, State: &state}))
	msgs := list(t, env, p.ID, "").Messages
	id := msgs[len(msgs)-1].ID
	if len(withRecord) > 0 && withRecord[0] {
		require.NoError(t, env.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO generation_approval (message_id, project_id, fingerprint, scope) VALUES (?, ?, 'fp1', '[]')`, id, p.ID)
			return err
		}))
	}
	return id
}

// A card of v0.1.34 can still be answered through respond_approval (the same service call as the button): no error, the Project is back in
// VISUAL_DESIGN where the Orchestrator starts the Shots, and the answer says the card is of the old policy (approve) or what to do next (decline). A card
// with no record (an imported bundle's) is said not to be taken as an approval, and its decline says only a message lifts it; the card says it too.
func TestRespondApproval_ALegacyGenerationCardCanStillBeAnswered(t *testing.T) {
	for _, withRecord := range []bool{true, false} {
		for decision, want := range map[string]string{"approve": "AUTO では承認を求めずに", "request_changes": mainline.DeclinedAck} {
			if !withRecord {
				want = map[string]string{"approve": "承認としては扱いません", "request_changes": mainline.DeclinedAckUnrecorded}[decision]
			}
			env := newEnv(t)
			p := createProject(t, env)
			cardID := legacyGenerationCard(t, env, p, withRecord)
			var card chat.MessageOutput
			env.CallOK("get_chat_message", map[string]any{"project_id": p.ID, "message_id": cardID}, &card)
			assert.Equal(t, !withRecord, card.Message.Unrecorded, decision)

			var out chat.MessageOutput
			env.CallOK("respond_approval", map[string]any{"project_id": p.ID, "message_id": cardID, "decision": decision}, &out)

			assert.Equal(t, "VISUAL_DESIGN", out.Project.State, decision)
			assert.Equal(t, !withRecord, out.Message.Unrecorded, decision)
			msgs := list(t, env, p.ID, "").Messages
			assert.Contains(t, msgs[len(msgs)-1].Text, want, "%s record=%v", decision, withRecord)
			assert.Contains(t, env.CallError("respond_approval", map[string]any{"project_id": p.ID, "message_id": cardID, "decision": decision}), "conflict")
		}
	}
}

// The system settles a pending card of the old policy so that an AUTO Project is not left stuck on it: the card is closed, the Project leaves
// AWAITING_APPROVAL, the thread says the system did it, and no answer of the user is made up. A card that is not pending is left alone.
func TestSupersede_ClosesAPendingLegacyCardAsTheSystemAndReturnsTheProject(t *testing.T) {
	ctx := context.Background()
	env := newEnv(t)
	p := createProject(t, env)
	cardID := legacyGenerationCard(t, env, p)
	before := len(list(t, env, p.ID, "").Messages)

	settled, err := env.Services.Chat.Supersede(ctx, p.ID, cardID, "システムがこのカードを閉じました")

	require.NoError(t, err)
	assert.True(t, settled)
	all := list(t, env, p.ID, "")
	assert.Equal(t, "VISUAL_DESIGN", all.Project.State)
	require.Len(t, all.Messages, before+1, "one notice of the assistant, and no message of the user")
	assert.Equal(t, "assistant", all.Messages[before].Role)
	assert.Contains(t, all.Messages[before].Text, "システムがこのカードを閉じました")
	var card chat.MessageOutput
	env.CallOK("get_chat_message", map[string]any{"project_id": p.ID, "message_id": cardID}, &card)
	assert.Equal(t, "approved", card.Message.ApprovalStatus)
	again, err := env.Services.Chat.Supersede(ctx, p.ID, cardID, "二度目")
	require.NoError(t, err)
	assert.False(t, again)
	assert.Len(t, list(t, env, p.ID, "").Messages, before+1)
}
