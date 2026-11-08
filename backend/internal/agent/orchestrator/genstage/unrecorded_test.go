package genstage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// hugeID sorts after every message ID this instance issues (UUIDv7): the ID a bundle can carry.
const hugeID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

// A bundle carries the chat cards of a Project but not generation_approval. A card with no record is never an approval (#1108, kept by #1116): outside
// AUTO nothing starts by itself whatever such a card says, and the answer on the card does not start a Shot or ask for a new card.
func TestGenerationApproval_AnApprovedCardWithoutARecordIsNeverTheApproval(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	cardID := r.legacyCard(id, domain.ApprovalApproved)
	r.withoutRecord(cardID)
	r.setMode(id, domain.ModeDirector)

	r.handle(id)
	r.handle(id)

	assert.Empty(t, r.gen.started(), "an approval that nobody can show starts no Reference Pack, Keyframe or Video")
	assert.Equal(t, 1, r.cards(id), "no new card is asked for")
	assert.Zero(t, r.announcements())
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseManual, phase, "the screen says the user starts each Shot")
}

// In an AUTO Project the same card changes nothing either: what starts the generation is AUTO (the consent), so the Project goes on exactly as one that
// never had a card, and no new card is asked for.
func TestUnrecordedCard_AnApprovedCardIsNotWhatStartsTheGenerationInAuto(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.withoutRecord(r.legacyCard(id, domain.ApprovalApproved))

	r.handle(id)

	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
	assert.Equal(t, 1, r.cards(id), "no new card is asked for")
	assert.Equal(t, 1, r.announcements(), "the start is announced as the AUTO consent")
}

// A pending card with no record in an AUTO Project is closed by the system like any other pending card of v0.1.34 (no answer of the user is made up);
// the generation that follows is AUTO's, not the card's.
func TestUnrecordedCard_APendingCardInAutoIsSettledBySystemAndTheGenerationIsAutos(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	cardID := r.legacyCard(id, domain.ApprovalPending)
	r.withoutRecord(cardID)

	r.handle(id)

	assert.Equal(t, []string{cardID}, r.chat.superseded)
	assert.Equal(t, 1, r.cards(id))
	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
}

// Outside AUTO a pending card with no record is left as it is, and answering it starts nothing: the user starts each Shot.
func TestUnrecordedCard_APendingCardOutsideAutoStartsNothing(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	cardID := r.legacyCard(id, domain.ApprovalPending)
	r.withoutRecord(cardID)
	r.setMode(id, domain.ModeDirector)

	r.handle(id)
	assert.Empty(t, r.chat.superseded)
	assert.Equal(t, domain.ApprovalPending, r.card(id).Status)

	r.answer(id, domain.ApprovalApproved)
	r.handle(id)
	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
	assert.Empty(t, r.gen.started())
	assert.Equal(t, 1, r.cards(id))
}

// A declined card with no record keeps the generation from starting, and the screen says that only a message lifts it (the plan cannot be compared with
// a record that is not there): a phase of its own, whose words do not promise that a change of the plan starts the generation.
func TestUnrecordedCard_ADeclinedCardInAutoSaysOnlyAMessageLiftsIt(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.withoutRecord(r.legacyCard(id, domain.ApprovalChangesRequested))

	r.handle(id)
	r.handle(id)

	assert.Empty(t, r.gen.started())
	assert.Equal(t, 1, r.cards(id), "no new card is asked for")
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseDeclinedUnrecorded, phase)
	wait := mainline.Wait{Shots: 3, Phase: phase}
	assert.NotContains(t, wait.NextAction(), "計画を直す（", "a change of the plan does not start it")
	assert.Equal(t, mainline.Wait{Shots: 3, Phase: mainline.PhaseDeclined}.ShotLabel(), wait.ShotLabel(), "the Shot is held back the same way")
}

// Outside AUTO the declined card with no record changes nothing: nothing starts, and the card stays declined.
func TestUnrecordedCard_ADeclinedCardOutsideAutoStartsNothingAndStaysDeclined(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.withoutRecord(r.legacyCard(id, domain.ApprovalChangesRequested))
	r.setMode(id, domain.ModeDirector)

	r.handle(id)
	r.userWrites(id, "進めて")
	r.handle(id)

	assert.Empty(t, r.gen.started(), "DIRECTOR never starts by itself, whatever the user writes")
	assert.Equal(t, domain.ApprovalChangesRequested, r.card(id).Status)
	assert.Equal(t, 1, r.cards(id))
}

// "Latest" is the order this instance issued the cards in, never the message ID that a bundle brings: an imported card with a huge ID does not stay the
// newest one for ever, so a card made here after it is the one the gate looks at.
func TestLatestCard_IsDecidedByLocalOrderNotByAnImportedID(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.legacyCardWithID(id, domain.ApprovalChangesRequested, hugeID)
	require.Equal(t, hugeID, r.card(id).ID)
	later := r.legacyCard(id, domain.ApprovalPending)

	latest := r.card(id)

	require.NotNil(t, latest)
	assert.Equal(t, later, latest.ID, "the card made later wins over the card with the huge ID")
	assert.Equal(t, domain.ApprovalPending, latest.Status)
}

// A decline whose card has a huge ID is lifted by a message of the user like any other: "after the card" is the local order too, so a message written
// here is after it even though its ID is smaller.
func TestLegacyCard_ADeclinedCardWithAHugeIDIsLiftedByAMessage(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.withoutRecord(r.legacyCardWithID(id, domain.ApprovalChangesRequested, hugeID))
	r.handle(id)
	require.Empty(t, r.gen.started())

	r.userWrites(id, "進めて")
	r.handle(id)

	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
}

// The script checkpoint finds its card by the same rule: of two cards for one script, the one made here later is the newest, whatever message IDs they bear.
func TestLatestApprovalCard_IsDecidedByLocalOrderNotByAnImportedID(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	imported := domain.ChatMessage{ID: hugeID, Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "脚本", RefType: domain.CheckpointScript.RefType(), RefID: "sv1", ApprovalStatus: domain.ApprovalPending}
	local := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindApproval, Content: "脚本", RefType: domain.CheckpointScript.RefType(), RefID: "sv1", ApprovalStatus: domain.ApprovalPending}
	require.NoError(t, r.projects.Apply(context.Background(), repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{imported}}))
	require.NoError(t, r.projects.Apply(context.Background(), repository.Turn{ProjectID: id, Messages: []domain.ChatMessage{local}}))

	latest, err := r.chatRepo.LatestApprovalCard(context.Background(), id, domain.CheckpointScript.RefType(), "sv1")

	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.NotEqual(t, hugeID, latest.ID)
}
