package genstage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/mainline"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// AUTO is the consent: right after the Scene / Shot plan the Orchestrator starts the Reference Packs by itself, at every Confirmation Level, and posts
// no approval card (spec/spec.md §4.3). The Project stays in VISUAL_DESIGN, where the Shots are made.
func TestAutoStart_NoApprovalCardAndTheGenerationStartsAtAnyConfirmationLevel(t *testing.T) {
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

			assert.Zero(t, r.cards(id), "no generation approval card, at any level")
			assert.Empty(t, r.chat.checkpoints, "nothing asked for an approval")
			assert.Equal(t, domain.StateVisualDesign, r.project1(id).State, "the Project is never held at a card")
			assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack), "a Reference Pack for each Shot, once")
			assert.Zero(t, r.gen.count(genapprovalrepo.StageGenerate), "a Shot is generated when its Reference Pack has been made")
			phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
			require.NoError(t, err)
			assert.Equal(t, mainline.PhaseAuto, phase)
		})
	}
}

// The whole path at FULL AUTO: Brief → script (no card at this level) → Scene / Shot plan → the Reference Packs, in one run of the Orchestrator.
// The notice of the plan says that the Orchestrator goes on by itself.
func TestAutoStart_FullAutoPlansAndStartsInOneRun(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.project("", domain.ModeAuto, domain.ConfirmFullAuto)

	r.handle(id)

	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
	assert.Empty(t, r.chat.checkpoints, "FULL AUTO asks for no approval at all")
	assert.Equal(t, 3, len(r.shotIDs(id)))
	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
	notices := r.chat.notices()
	assert.Contains(t, notices[0], "Scene / Shot の計画ができました")
	assert.Contains(t, notices[0], "承認を待たずに")
	assert.Equal(t, 1, r.announcements())
}

// KEY CHECK keeps its own checkpoints: the script card first (the Orchestrator waits for it), and the generation starts by itself after it is approved.
func TestAutoStart_KeyCheckKeepsTheScriptCardAndThenStartsByItself(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.project("", domain.ModeAuto, domain.ConfirmKeyCheck)
	r.handle(id)
	require.Equal(t, []domain.Checkpoint{domain.CheckpointScript}, r.chat.checkpoints)
	require.Empty(t, r.gen.started(), "the script is not approved yet")
	script, err := r.plans.LatestScript(context.Background(), id)
	require.NoError(t, err)
	scriptCard, err := r.chatRepo.LatestApprovalCard(context.Background(), id, domain.CheckpointScript.RefType(), script.ID)
	require.NoError(t, err)
	r.approveCard(id, scriptCard.ID, domain.CheckpointScript.ApprovedState())

	r.handle(id)

	assert.Equal(t, []domain.Checkpoint{domain.CheckpointScript}, r.chat.checkpoints, "no generation card follows the script card")
	assert.Zero(t, r.cards(id))
	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
}

// DIRECTOR (and MANUAL) Projects: the gate does nothing and no bypass is introduced; the user starts each Shot as before.
func TestAutoStart_DirectorModeStartsNothingAndAsksNothing(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.project(domain.StateVisualDesign, domain.ModeDirector, domain.ConfirmFullAuto)
	var plan domain.ScenePlan
	plan.Scenes = []domain.PlannedScene{{Title: "S", Shots: []domain.PlannedShot{{Objective: "o", DurationSeconds: 5}}}}
	require.NoError(t, r.plans.SavePlan(context.Background(), id, plan, domain.Storyboard{Shots: [][]domain.ShotStoryboard{{{Description: "f"}}}}))

	require.NoError(t, r.orch.Kick(context.Background(), id))
	r.handle(id)

	assert.Zero(t, r.planJobs.count(), "no plan job is started for a DIRECTOR Project")
	assert.Zero(t, r.cards(id))
	assert.Empty(t, r.gen.started())
	assert.Zero(t, r.announcements())
	assert.Equal(t, domain.StateVisualDesign, r.project1(id).State)
	phase, _, err := r.gate.Phase(context.Background(), r.project1(id))
	require.NoError(t, err)
	assert.Equal(t, mainline.PhaseManual, phase)
}

// Switching a DIRECTOR Project to AUTO is the consent too: the Orchestrator looks again and starts what the plan holds.
func TestAutoStart_SwitchingToAutoStartsThePlannedShots(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	r.setMode(id, domain.ModeDirector)
	r.handle(id)
	require.Empty(t, r.gen.started())

	r.setMode(id, domain.ModeAuto)
	r.handle(id)

	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
}

// The Scene / Shot links are what the start is made of: Script → Scene → Shot, and the Shots started are the ones of the plan in order.
func TestAutoStart_StartsTheShotsOfTheScenesInOrder(t *testing.T) {
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
	shots := r.shotIDs(id)
	require.Len(t, shots, 3)
	var want []call
	for _, sh := range shots {
		ref, err := r.gens.GetShot(ctx, sh)
		require.NoError(t, err)
		assert.Equal(t, id, ref.ProjectID)
		assert.Equal(t, scenes[0].ID, ref.Scene.ID, "the Shot belongs to its Scene")
		want = append(want, call{genapprovalrepo.StageReferencePack, sh})
	}
	assert.Equal(t, want, r.gen.started())
}

// The announcement is posted once, when the Orchestrator starts: it says what is started, what it is estimated to cost ("不明" for a price it does not
// know, never a made-up number), that an unknown price does not stop it, that no budget ceiling is set, and how to stop it.
func TestAutoStart_AnUnknownPriceStartsAndTheOneAnnouncementSaysUnknown(t *testing.T) {
	t.Parallel()
	r := newRig(t) // GPT Image 2.6 has no price in the default setting: the Reference Pack cannot be priced
	id := r.planned(domain.ConfirmFullAuto)

	r.handle(id)
	r.handle(id)
	r.restart()
	r.handle(id)

	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack), "an estimate that is 不明 does not stop the start")
	require.Equal(t, 1, r.announcements(), "one announcement for the start, not one per look or restart")
	text := r.announcement()
	for _, want := range []string{
		"AUTO を選ぶことが、生成を始める同意です", "費用がかかります",
		"Shot 3 件（Shot 1・2・3）",
		"Reference Pack を作る Shot: 3 件", "gpt-image-2.6-flare / nano-banana",
		"seedance-2.0（720p）", "合計最大 9 本",
		"費用の見積: 不明", "単価が不明でも止めずに始めます",
		"予算上限: Project 設定なし / Shot あたり 設定なし", "予算上限が未設定で、単価が分からない分があります",
		"止めるには", "予算上限", "DIRECTOR",
	} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "費用の見積: 最大")
}

// With every price known the announcement shows numbers, and the budget ceilings when they are set; a unit-priceless local model counts as 0 USD.
func TestAutoStart_AKnownPriceShowsNumbersAndTheBudgetCeilings(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.prices.pricesKnown()
	id := r.planned(domain.ConfirmFullAuto)
	require.NoError(t, r.policies.SetBudget(context.Background(), id, new(50.0), new(5.0)))

	r.handle(id)

	text := r.announcement()
	for _, want := range []string{
		"費用の見積: 最大 ", "USD（Reference Pack 最大 ", "動画 最大 6.7500 USD", // 9 clips × 5 s × 0.15 USD
		"予算上限: Project 50.0000 USD", "Shot あたり 5.0000 USD", "含まれないもの",
	} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "予算上限が未設定", "a ceiling is set: the risk is not the open-ended one")
	assert.NotContains(t, text, "不明")
}

// The announcement is made for the Shots the Orchestrator starts for the first time: the generations that follow the Reference Packs, and a restart in the
// middle, do not announce again; a Shot that was added later is announced when it is started.
func TestAutoStart_AnnouncesOncePerStartAndAgainOnlyForNewShots(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.handle(id)
	require.Equal(t, 1, r.announcements())

	for _, s := range shots {
		r.endJob(s, false)
	}
	r.restart()
	r.handle(id)
	require.Equal(t, 3, r.gen.count(genapprovalrepo.StageGenerate))
	assert.Equal(t, 1, r.announcements(), "the generations of the Shots whose pack is made are the same start")

	scenes, err := r.plans.ListScenes(context.Background(), id)
	require.NoError(t, err)
	_, err = r.plans.CreateShot(context.Background(), domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
	require.NoError(t, err)
	r.handle(id)

	assert.Equal(t, 2, r.announcements(), "a Shot added to the plan is a new start")
	assert.Contains(t, r.chat.notices()[len(r.chat.notices())-1], "対象: Shot 1 件（Shot 4）", "the announcement is of the Shot that is new")
}
