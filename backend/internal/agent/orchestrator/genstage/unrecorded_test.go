package genstage_test

import (
	"context"
	"fmt"
	"slices"
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

// seen is what the gate finds and does for a Project at one look (the verdict, the phase, the starts it would make, the stuck Shots) and what the Orchestrator
// then started, with every Shot named by its number: the Shot IDs differ between two Projects.
func (r *rig) seen(id string) string {
	r.t.Helper()
	ctx := context.Background()
	number := map[string]int{}
	for i, shotID := range r.shotIDs(id) {
		number[shotID] = i + 1
	}
	a, err := r.gate.Assess(ctx, r.project1(id))
	require.NoError(r.t, err)
	phase, stuck, err := r.gate.Phase(ctx, r.project1(id))
	require.NoError(r.t, err)
	var next, stuckNumbers, started []string
	for _, st := range a.Next {
		next = append(next, fmt.Sprintf("%s#%d", st.Stage, number[st.Line.ShotID]))
	}
	for shotID := range stuck {
		stuckNumbers = append(stuckNumbers, fmt.Sprint(number[shotID]))
	}
	slices.Sort(stuckNumbers)
	before := len(r.gen.started())
	r.handle(id)
	for _, c := range r.gen.started()[before:] {
		started = append(started, fmt.Sprintf("%s#%d", c.stage, number[c.shotID]))
	}
	return fmt.Sprintf("verdict=%d phase=%q next=%v stuck=%v started=%v", a.Verdict, phase, next, stuckNumbers, started)
}

// budgetStop leaves the first Shot of the Project stopped by the budget ceiling, as the gate does when a start would pass it.
func (r *rig) budgetStop(id string) {
	r.t.Helper()
	shotID := r.shotIDs(id)[0]
	claimed, err := r.launches.Claim(context.Background(), shotID, genapprovalrepo.StageReferencePack)
	require.NoError(r.t, err)
	require.True(r.t, claimed)
	require.NoError(r.t, r.launches.Stopped(context.Background(), shotID, genapprovalrepo.StageReferencePack, genapprovalrepo.OutcomeBudget))
}

// addShot adds a Shot to the Scene / Shot plan of the Project.
func (r *rig) addShot(id string) {
	r.t.Helper()
	scenes, err := r.plans.ListScenes(context.Background(), id)
	require.NoError(r.t, err)
	_, err = r.plans.CreateShot(context.Background(), domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
	require.NoError(r.t, err)
}

// A bundle carries the chat cards of a Project but not generation_approval. This pins the invariant that a card is never the start condition (#1108, kept
// by #1116): a card with no record, whatever it says, is not an approval that anybody can show.
//
// In AUTO the Project must go on exactly as one that never had a card: the same verdict, the same phase, the same starts, the same stuck Shots, and no new
// card is asked for. This is compared with a twin Project that has no card, so the test fails when the card changes anything (it would, were it to start
// the generation by itself, hold it back, or stand in for the consent). Outside AUTO, where the gate returns an empty assessment anyway, the same card
// starts nothing and asks for nothing either.
func TestGenerationApproval_AnApprovedCardWithoutARecordIsNeverTheApproval(t *testing.T) {
	t.Parallel()
	prepare := map[string]func(r *rig, id string){
		"the plan as the card was made":        func(*rig, string) {},
		"a Shot stopped by the budget ceiling": (*rig).budgetStop,
		"a changed plan":                       (*rig).addShot,
	}
	for name, change := range prepare {
		t.Run("AUTO, "+name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			bare := r.planned(domain.ConfirmFullAuto)
			carded := r.planned(domain.ConfirmFullAuto)
			r.withoutRecord(r.legacyCard(carded, domain.ApprovalApproved))
			change(r, bare)
			change(r, carded)

			want := r.seen(bare)
			got := r.seen(carded)

			require.NotContains(t, want, "next=[]", "the twin has starts to make: the comparison has something to compare")
			assert.Equal(t, want, got, "a card with no record changes nothing")
			assert.Equal(t, 1, r.cards(carded), "no new card is asked for")
		})
	}

	t.Run("DIRECTOR", func(t *testing.T) {
		t.Parallel()
		r := newRig(t)
		id := r.planned(domain.ConfirmFullAuto)
		r.withoutRecord(r.legacyCard(id, domain.ApprovalApproved))
		r.setMode(id, domain.ModeDirector)

		r.handle(id)
		r.handle(id)

		assert.Empty(t, r.gen.started(), "an approval that nobody can show starts no Reference Pack, Keyframe or Video")
		assert.Equal(t, 1, r.cards(id), "no new card is asked for")
		phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
		require.NoError(t, err)
		assert.Equal(t, mainline.PhaseManual, phase, "the screen says the user starts each Shot")
	})
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
