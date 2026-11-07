package genstage_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

// After the approval the Orchestrator goes on by itself, in order: every Shot without a Keyframe gets its Reference Pack first; a Shot's generation
// is started when its Reference Pack job has succeeded, and not before.
func TestGenerationApproval_AfterTheApprovalReferencePacksThenShotsStartByThemselves(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.approved(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	require.Empty(t, r.gen.started(), "answering does not start anything by itself: the Orchestrator does, at its next look")

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
func TestGenerationApproval_AShotWithAKeyframeIsGeneratedWithoutAPack(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	spec, _ := json.Marshal(map[string]any{"references": []domain.Reference{{Kind: domain.RefKeyframe, AssetID: "a", Prompt: "p"}}})
	require.NoError(t, r.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(spec), shots[0])
		return err
	}))
	r.handle(id)
	r.answer(id, domain.ApprovalApproved)

	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageGenerate, shots[0]}, {genapprovalrepo.StageReferencePack, shots[1]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started())
}

// The same approval never starts a Shot twice: not when the Gate looks again, not after the Shot was released, not in a process started again.
func TestGenerationApproval_TheSameApprovalNeverStartsAShotTwice(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.approved(domain.ConfirmFullAuto)
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
func TestGenerationApproval_ABusyShotAndAShotWithATakeAreLeftAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRig(t)
	id := r.approved(domain.ConfirmFullAuto)
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

// A start that fails, and a Reference Pack that failed, leave the Shot to the user: the failure is told, it is not tried again, and the Shots
// beside it go on.
func TestGenerationApproval_AFailedStartIsToldAndLeavesThatShotToTheUser(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.approved(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.gen.errs[shots[0]] = errBoom

	r.handle(id)
	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[1]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started())
	var told int
	for _, n := range r.chat.notices() {
		if strings.Contains(n, "Shot 1 の Reference Pack を自動では始められませんでした") {
			told++
		}
	}
	assert.Equal(t, 1, told, "told once")

	r.endJob(shots[1], true) // the Reference Pack of Shot 2 failed
	r.endJob(shots[2], false)
	r.handle(id)
	assert.Equal(t, []call{{genapprovalrepo.StageGenerate, shots[2]}}, r.gen.started()[2:], "a Shot whose pack failed is not generated by itself")
	phase, stuck, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseAuto, phase)
	assert.True(t, stuck[shots[0]] && stuck[shots[1]], "both are the user's to start")
}

// The budget ceiling is not an approval, and it still stops: a start it refuses is an Escalation (once), the Shot is not tried again by itself.
func TestGenerationApproval_TheBudgetCeilingStillStopsAnApprovedStartWithAnEscalation(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(500.0), nil))
	r.handle(id)
	require.NotNil(t, r.card(id), "a ceiling that is not reached does not take the approval's place")
	require.Empty(t, r.gen.started())
	r.answer(id, domain.ApprovalApproved)
	shots := r.shotIDs(id)
	r.gen.errs[shots[1]] = generation.ErrBudgetExceeded

	r.handle(id)
	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[0]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started())
	require.Len(t, r.chat.escalations, 1, "one Escalation for the start the ceiling refused, not one per look")
	assert.Contains(t, r.chat.escalations[0], "Shot 2 の Reference Pack")
	assert.Contains(t, r.chat.escalations[0], "予算上限")
}
