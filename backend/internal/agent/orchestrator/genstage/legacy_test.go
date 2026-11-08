package genstage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/orchestrator/genstage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// A Project of v0.1.34 can hold a generation approval card that was never answered. The Orchestrator settles it as the system (a notice of the assistant,
// no message of the user), the Project returns from AWAITING_APPROVAL to VISUAL_DESIGN, and the generation starts in the same run: an AUTO Project is
// never left stuck on an obsolete card.
func TestLegacyCard_APendingCardIsSettledBySystemAndTheGenerationStarts(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	cardID := r.legacyCard(id, domain.ApprovalPending)
	require.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)

	r.handle(id)

	assert.Equal(t, []string{cardID}, r.chat.superseded)
	assert.Equal(t, domain.ApprovalApproved, r.card(id).Status)
	assert.Equal(t, 1, r.cards(id), "no new card")
	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
	var notices int
	require.NoError(t, r.db.Reader().QueryRow(`SELECT COUNT(*) FROM chat_message WHERE project_id = ? AND role = 'assistant' AND content = ?`, id, genstage.SupersededNotice).Scan(&notices))
	assert.Equal(t, 1, notices, "the thread records that the system closed the card")
	assert.Contains(t, genstage.SupersededNotice, "あなたの回答ではありません")
	var userReplies int
	require.NoError(t, r.db.Reader().QueryRow(`SELECT COUNT(*) FROM chat_message WHERE project_id = ? AND role = 'user' AND reply_to_message_id = ?`, id, cardID).Scan(&userReplies))
	assert.Zero(t, userReplies, "the system did not make up an answer of the user")
	r.handle(id)
	assert.Equal(t, 1, len(r.chat.superseded), "settled once")
}

// An answer to the old card that arrives while the Orchestrator settles it wins: the card is not settled twice and nothing is overwritten.
func TestLegacyCard_ACardAnsweredMeanwhileIsLeftAsAnswered(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	cardID := r.legacyCard(id, domain.ApprovalPending)
	r.answer(id, domain.ApprovalApproved)

	settled, err := r.chat.Supersede(context.Background(), id, cardID, genstage.SupersededNotice)

	require.NoError(t, err)
	assert.False(t, settled)
	assert.Empty(t, r.chat.superseded)
}

// A DIRECTOR Project is left alone: the card stays pending and can still be answered by the user.
func TestLegacyCard_ADirectorProjectKeepsItsPendingCard(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	cardID := r.legacyCard(id, domain.ApprovalPending)
	r.setMode(id, domain.ModeDirector)

	r.handle(id)

	assert.Empty(t, r.chat.superseded)
	assert.Equal(t, domain.ApprovalPending, r.card(id).Status)
	assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)
	assert.Empty(t, r.gen.started())

	r.answer(id, domain.ApprovalApproved) // the user answers the old card
	assert.Equal(t, domain.ApprovalApproved, r.card(id).Status)
	assert.Equal(t, cardID, r.card(id).ID)
	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
}

// A card the user declined keeps meaning "do not generate yet": nothing starts by itself, however often the Orchestrator looks, and the screen says so.
func TestLegacyCard_ADeclinedCardKeepsTheGenerationFromStarting(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.legacyCard(id, domain.ApprovalChangesRequested)

	r.handle(id)
	r.handle(id)
	r.restart()
	r.handle(id)

	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
	assert.Equal(t, 1, r.cards(id), "no new card is asked for")
	assert.Empty(t, r.gen.started(), "a decline is never overridden by AUTO")
	assert.Zero(t, r.announcements())
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseDeclined, phase)
}

// After a decline the user can let the generation go on by writing to the thread (not as a reply to the card): the Orchestrator starts, without a new card.
func TestLegacyCard_ADeclinedCardIsLiftedWhenTheUserWrites(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.legacyCard(id, domain.ApprovalChangesRequested)
	r.handle(id)
	require.Empty(t, r.gen.started())

	r.userWrites(id, "やっぱり進めて")
	r.handle(id)

	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
	assert.Equal(t, 1, r.cards(id), "the card is not asked again")
	assert.Equal(t, 1, r.announcements())
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseAuto, phase)
}

// A decline stands for the plan it was given for: when the plan changes (a Shot is added) the Orchestrator starts the plan as it is now.
func TestLegacyCard_ADeclinedCardIsLiftedWhenThePlanChanges(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.legacyCard(id, domain.ApprovalChangesRequested)
	r.handle(id)
	require.Empty(t, r.gen.started())
	scenes, err := r.plans.ListScenes(context.Background(), id)
	require.NoError(t, err)
	_, err = r.plans.CreateShot(context.Background(), domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
	require.NoError(t, err)

	r.handle(id)

	assert.Equal(t, 4, r.gen.count(genapprovalrepo.StageReferencePack))
	assert.Equal(t, 1, r.cards(id))
}

// Answering the reply to the card is not "writing again": the decline's own answer does not lift it.
func TestLegacyCard_TheAnswerToTheCardItselfDoesNotLiftTheDecline(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.legacyCard(id, domain.ApprovalChangesRequested) // the helper answers with a reply to the card

	r.handle(id)

	assert.Empty(t, r.gen.started())
}

// A bundle carries the chat cards of a Project but not generation_approval (the record is of this instance's models and prices). A declined card that
// arrives without its record cannot be compared with the plan, so a change of the plan does not lift it; only a message of the user does.
func TestLegacyCard_ADeclinedCardWithoutARecordIsLiftedByAMessageOnly(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	cardID := r.legacyCard(id, domain.ApprovalChangesRequested)
	r.withoutRecord(cardID)
	scenes, err := r.plans.ListScenes(context.Background(), id)
	require.NoError(t, err)
	_, err = r.plans.CreateShot(context.Background(), domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
	require.NoError(t, err)

	r.handle(id)
	assert.Empty(t, r.gen.started(), "no record to compare: the plan changing says nothing")

	r.userWrites(id, "進めて")
	r.handle(id)
	assert.Equal(t, 4, r.gen.count(genapprovalrepo.StageReferencePack))
}
