package genstage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Nothing is generated before the approval: after the Scene / Shot plan the Gate posts the card at every Confirmation Level (FULL AUTO too), the
// Project waits in AWAITING_APPROVAL, and looking again starts nothing and posts no second card.
func TestGenerationApproval_NothingStartsBeforeItIsGivenAtAnyConfirmationLevel(t *testing.T) {
	t.Parallel()
	for name, level := range map[string]domain.ConfirmationLevel{
		"FULL AUTO": domain.ConfirmFullAuto, "KEY CHECK": domain.ConfirmKeyCheck, "CUSTOM CHECK with nothing chosen": domain.ConfirmCustomCheck,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			id := r.planned(level)

			r.handle(id)
			r.handle(id)

			card := r.card(id)
			require.NotNil(t, card, "the generation approval is asked at every level")
			assert.Equal(t, domain.ApprovalPending, card.Status)
			assert.Equal(t, 1, r.cards(id), "a pending card is not asked again")
			assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)
			assert.Empty(t, r.gen.started(), "no Reference Pack, Keyframe or Video before the approval")
			assert.Zero(t, r.planJobs.count())
		})
	}
}

// The whole path at FULL AUTO: Brief → script (no card at this level) → Scene / Shot plan → the generation approval card, whose notice and body say
// what is waited for. The script, Rough Cut and Final checkpoints keep following the Confirmation Level.
func TestGenerationApproval_FullAutoPlansThenAsksAndTheNoticeSaysSo(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.project("", domain.ModeAuto, domain.ConfirmFullAuto)

	r.handle(id)

	assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)
	assert.Equal(t, []domain.Checkpoint{domain.CheckpointGeneration}, r.chat.checkpoints, "FULL AUTO asks for no script approval, but always for the generation one")
	notices := r.chat.notices()
	plan := notices[len(notices)-1]
	assert.Contains(t, plan, "Scene / Shot の計画ができました")
	assert.Contains(t, plan, "生成の承認を待っています")
	assert.Contains(t, plan, "FULL AUTO でも、この承認は必ず求めます")
	assert.Empty(t, r.gen.started())
	assert.Equal(t, 3, len(r.shotIDs(id)))
}

// KEY CHECK keeps the script approval and the generation approval apart: the script card first, the generation card after the plan.
func TestGenerationApproval_KeyCheckKeepsTheScriptApprovalAndAddsTheGenerationOne(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.project("", domain.ModeAuto, domain.ConfirmKeyCheck)
	r.handle(id)
	require.Equal(t, []domain.Checkpoint{domain.CheckpointScript}, r.chat.checkpoints)
	require.Nil(t, r.card(id))
	script, err := r.plans.LatestScript(context.Background(), id)
	require.NoError(t, err)
	scriptCard, err := r.chatRepo.LatestApprovalCard(context.Background(), id, domain.CheckpointScript.RefType(), script.ID)
	require.NoError(t, err)
	require.NoError(t, r.projects.Apply(context.Background(), repository.Turn{
		ProjectID: id, Approval: &repository.ApprovalUpdate{MessageID: scriptCard.ID, Status: domain.ApprovalApproved},
		ResumeState: new(domain.CheckpointScript.ApprovedState()),
	}))

	r.handle(id)

	assert.Equal(t, []domain.Checkpoint{domain.CheckpointScript, domain.CheckpointGeneration}, r.chat.checkpoints)
	assert.NotNil(t, r.card(id))
	assert.Empty(t, r.gen.started())
}

// Declining does not start anything and does not ask again by itself; the card stays declined and the screen says so.
func TestGenerationApproval_DecliningStartsNothingAndWaitsDeclined(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.handle(id)

	r.answer(id, domain.ApprovalChangesRequested)
	r.handle(id)
	r.handle(id)

	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
	assert.Equal(t, domain.ApprovalChangesRequested, r.card(id).Status)
	assert.Equal(t, 1, r.cards(id), "a declined plan that did not change is not asked again")
	assert.Empty(t, r.gen.started(), "a decline never starts a generation")
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseDeclined, phase)
}

// After a decline the user can ask for the approval again without changing the plan: a message of theirs in the thread (not the reply to the
// card) does it, and the new card is the one that stands.
func TestGenerationApproval_ADeclinedPlanIsAskedAgainWhenTheUserWrites(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.handle(id)
	r.answer(id, domain.ApprovalChangesRequested)
	r.handle(id)
	require.Equal(t, 1, r.cards(id))

	require.NoError(t, r.projects.Apply(context.Background(), repository.Turn{ProjectID: id,
		Messages: []domain.ChatMessage{{Role: domain.RoleUser, Kind: domain.KindText, Content: "やっぱりもう一度見せて"}}}))
	r.handle(id)

	assert.Equal(t, 2, r.cards(id))
	assert.Equal(t, domain.ApprovalPending, r.card(id).Status)
	assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)
	assert.Empty(t, r.gen.started())
}

// Declining and then changing the plan (a Shot is added) asks again with the new scope, and says why.
func TestGenerationApproval_ADeclinedPlanThatChangedIsAskedAgain(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.handle(id)
	r.answer(id, domain.ApprovalChangesRequested)
	scenes, err := r.plans.ListScenes(context.Background(), id)
	require.NoError(t, err)
	_, err = r.plans.CreateShot(context.Background(), domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
	require.NoError(t, err)

	r.handle(id)

	assert.Equal(t, 2, r.cards(id))
	assert.Equal(t, domain.ApprovalPending, r.card(id).Status)
	assert.Empty(t, r.gen.started())
	pending, ok, err := r.launches.Approval(context.Background(), r.card(id).ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Contains(t, string(pending.Scope), `"number":4`, "the new card lists the Shot that was added")
}

// DIRECTOR (and MANUAL) Projects are not asked and nothing is started: the user starts each Shot as before.
func TestGenerationApproval_DirectorModeAsksNothingAndStartsNothing(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.project(domain.StateVisualDesign, domain.ModeDirector, domain.ConfirmFullAuto)
	var plan domain.ScenePlan
	plan.Scenes = []domain.PlannedScene{{Title: "S", Shots: []domain.PlannedShot{{Objective: "o", DurationSeconds: 5}}}}
	require.NoError(t, r.plans.SavePlan(context.Background(), id, plan, domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "f"}}}}))

	require.NoError(t, r.orch.Kick(context.Background(), id))
	r.handle(id)

	assert.Zero(t, r.planJobs.count(), "no plan job is started for a DIRECTOR Project")
	assert.Nil(t, r.card(id))
	assert.Empty(t, r.gen.started())
	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseManual, phase)
}

// The Scene / Shot links are what the approval is made of: Script → Scene → Shot, and the Shots the card lists are the ones of the plan in order.
func TestGenerationApproval_TheCardListsTheShotsOfTheScenesInOrder(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.project("", domain.ModeAuto, domain.ConfirmFullAuto)
	r.handle(id)

	ctx := context.Background()
	script, err := r.plans.LatestScript(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, len(script.Doc.Script.Scenes), "the script the plan was made from")
	scenes, err := r.plans.ListScenes(ctx, id)
	require.NoError(t, err)
	require.Len(t, scenes, 1)
	shots, err := r.plans.ListShots(ctx, scenes[0].ID)
	require.NoError(t, err)
	require.Len(t, shots, 3)
	approval, ok, err := r.launches.Approval(ctx, r.card(id).ID)
	require.NoError(t, err)
	require.True(t, ok)
	for i, sh := range shots {
		ref, err := r.gens.GetShot(ctx, sh.ID)
		require.NoError(t, err)
		assert.Equal(t, id, ref.ProjectID)
		assert.Equal(t, scenes[0].ID, ref.Scene.ID, "the Shot belongs to its Scene")
		assert.Contains(t, string(approval.Scope), `"shot_id":"`+sh.ID+`","number":`+string(rune('1'+i)), "the card lists Shot %d", i+1)
	}
}
