package genstage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// The Orchestrator goes on by itself, in order: every Shot without a Keyframe gets its Reference Pack first; a Shot's generation is started when its Reference Pack job has succeeded, and not before.
func TestAutoStart_ReferencePacksThenShotsStartByThemselves(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)

	r.handle(id)

	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack), "a Reference Pack for each Shot")
	assert.Zero(t, r.gen.count(genapprovalrepo.StageGenerate), "no Shot is generated while its Reference Pack is being made")
	r.handle(id)
	assert.Zero(t, r.gen.count(genapprovalrepo.StageGenerate), "a pack job that has not ended is waited for")

	r.endJob(shots[0], false)
	r.endJob(shots[1], false)
	r.handle(id)
	assert.Equal(t, []call{{genapprovalrepo.StageGenerate, shots[0]}, {genapprovalrepo.StageGenerate, shots[1]}}, r.gen.started()[3:], "the Shots whose pack is made are generated, in order")

	r.endJob(shots[2], false)
	r.handle(id)
	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageGenerate))
	assert.Equal(t, 6, len(r.gen.started()))
}

// A Shot that has a Keyframe (a Reference Pack was made) needs no pack: it is generated at once.
func TestAutoStart_AShotWithAKeyframeIsGeneratedWithoutAPack(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.withKeyframe(shots[0])

	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageGenerate, shots[0]}, {genapprovalrepo.StageReferencePack, shots[1]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started())
}

// A Shot is never started twice: not when the Gate looks again, not after the Shot was released, not in a process started again.
func TestAutoStart_AShotIsNeverStartedTwice(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)

	r.handle(id)
	r.handle(id)
	r.restart()
	r.handle(id)
	assert.Equal(t, 3, len(r.gen.started()), "three packs, once each, across looks and a restart")

	for _, s := range shots {
		r.endJob(s, false)
	}
	r.handle(id)
	r.handle(id)
	r.restart()
	r.handle(id)
	assert.Equal(t, 6, len(r.gen.started()), "three generations, once each")

	for _, s := range shots {
		r.endJob(s, false) // each Shot is released PLANNING with no Take (the generation failed or made nothing)
	}
	r.handle(id)
	r.restart()
	r.handle(id)
	assert.Equal(t, 6, len(r.gen.started()), "a Shot that was started is not started again, whatever became of it")
}

// A Shot that is being made by somebody else (started by hand), and one that has a Take, are not started; the claim of the Shot decides who owns it.
func TestAutoStart_ABusyShotAndAShotWithATakeAreLeftAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	claimed, err := r.gens.StartShot(ctx, shots[0]) // the user pressed 生成 on Shot 1
	require.NoError(t, err)
	require.True(t, claimed)
	pv, err := r.gens.SavePromptedSpec(ctx, shots[1], domain.ShotSpec{ShotObjective: "x", Camera: domain.SpecCamera{Framing: "wide"}}, nil, nil)
	require.NoError(t, err)
	_, err = r.gens.CreateTake(ctx, repository.NewTake{
		ProjectID: id, ShotID: shots[1], PromptVersionID: pv.ID, Model: domain.ModelSeedance2,
		Asset: domain.Asset{ID: "asset-1", ProjectID: id, Type: domain.AssetVideo, FilePath: "p/asset-1.mp4", MimeType: "video/mp4", SizeBytes: 3},
	})
	require.NoError(t, err)

	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started(), "only the Shot nobody is on and that has no Take")
}

// A Reference Pack that failed, and a start that fails (a model that cannot be used is refused at the start like this), leave that Shot to the user: the reason is told once, it is not tried again, and the Shots beside it go on.
func TestAutoStart_AFailedStartIsToldWithItsReasonAndLeavesThatShotToTheUser(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.gen.errs[shots[0]] = errBoom

	r.handle(id)
	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[1]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started())
	var told int
	for _, n := range r.chat.notices() {
		if strings.Contains(n, "Shot 1 の Reference Pack を自動では始められませんでした: "+generation.FailureReason(errBoom)) {
			told++
		}
	}
	assert.Equal(t, 1, told, "told once, with the reason")

	r.endJob(shots[1], true) // the Reference Pack of Shot 2 failed
	r.endJob(shots[2], false)
	r.handle(id)
	assert.Equal(t, []call{{genapprovalrepo.StageGenerate, shots[2]}}, r.gen.started()[2:], "a Shot whose pack failed is not generated by itself")
	phase, stuck, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseAuto, phase)
	assert.True(t, stuck[shots[0]] && stuck[shots[1]], "both are the user's to start")
}

// The budget ceiling is not an approval, and AUTO does not lift it: the first start it refuses is an Escalation (once), the Shot is not tried again by itself, and nothing further starts while the Project waits for the answer. After the answer the Shots that were not started yet go on.
func TestAutoStart_TheBudgetCeilingStopsTheStartWithOneEscalationAndNothingFurtherStarts(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(500.0), nil))
	shots := r.shotIDs(id)
	r.gen.errs[shots[1]] = generation.ErrBudgetExceeded

	r.handle(id)
	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[0]}}, r.gen.started(), "Shot 1 started; Shot 2 was refused; Shot 3 waits for the answer")
	require.Len(t, r.chat.escalations, 1, "one Escalation for the start the ceiling refused, not one per look")
	assert.Contains(t, r.chat.escalations[0], "Shot 2 の Reference Pack")
	assert.Contains(t, r.chat.escalations[0], "予算上限")
	assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State, "the Escalation holds the Project: the gate starts nothing")
	phase, stuck, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseAuto, phase, "an AUTO Project held at the budget Escalation is not told to be manual")
	assert.True(t, stuck[shots[1]], "the refused Shot is the user's to start")
	assert.False(t, stuck[shots[0]] || stuck[shots[2]])

	state := domain.EscalationResumeState // the user answered the Escalation
	require.NoError(t, r.projects.Apply(context.Background(), repository.Turn{ProjectID: id, State: &state}))
	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[0]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started(),
		"after the answer the Shot that was not started goes on, and the refused one is not tried again by itself")
	assert.Len(t, r.chat.escalations, 1)
}

// A plan that passes the ceiling before anything was started is held by the Escalation too: the Project is AUTO, so the Scene / Shot screen is not told that it is manual.
func TestAutoStart_APlanOverTheCeilingHeldBeforeAnyStartIsStillAnAutoProject(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.prices.pricesKnown()
	id := r.planned(domain.ConfirmFullAuto)
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(0.5), nil))

	r.handle(id)

	require.Len(t, r.chat.escalations, 1, "the plan costs more than the ceiling: one Escalation")
	assert.Empty(t, r.gen.started(), "nothing was started")
	assert.Equal(t, domain.StateAwaitingApproval, r.project1(id).State)
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseAuto, phase)
}

// Cancelling while the Gate starts (the job is cancelled, the process stops) starts nothing more and gives the claim back, so that the next look starts the Shot, once.
func TestAutoStart_CancellationStopsTheStartAndReleasesTheClaim(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	ctx, cancel := context.WithCancel(context.Background())
	r.gen.cancelOn, r.gen.cancel = shots[1], cancel

	_, err := r.gate.Step(ctx, r.project1(id))

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[0]}}, r.gen.started(), "the Shot after the cancelled one is not started")
	launches, err := r.launches.Launches(context.Background(), id)
	require.NoError(t, err)
	assert.Contains(t, launches, shots[0])
	assert.NotContains(t, launches, shots[1], "the claim of the cancelled start is given back")

	r.gen.cancelOn = ""
	r.handle(id)
	assert.Equal(t, 3, len(r.gen.started()), "Shot 1 is not started again; Shots 2 and 3 are")
}
