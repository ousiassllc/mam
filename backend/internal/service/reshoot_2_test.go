package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// A Partial Take Use makes the joined Take the Shot's Selected Take: like Select, it must wake the
// Editor → Reshoot Loop, or the round whose last Shot was finished that way waits for an unrelated
// trigger (#327).
func TestEditorReshootLoop_APartialTakeUseOfTheLastShotReturnsToTheEditor(t *testing.T) {
	f, judged := editedFixtureWith(t, needReaction, func(string) string { return additionalSpec }, nil, useStubMedia(t))
	ctx := context.Background()
	newShot := f.additionalShot(t)
	f.execSQL(t, `UPDATE project SET mode = 'director' WHERE id = ?`, f.projectID)
	var take string
	require.NoError(t, f.query(t, `SELECT t.id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ? ORDER BY t.rowid LIMIT 1`, newShot).Scan(&take))
	require.Equal(t, domain.StateGenerating, f.state(t))
	_, err := f.svc.Takes.UsePartial(ctx, newShot, service.PartialInput{Segments: []domain.TakeSegment{{TakeID: take, Start: 0, End: 1}, {TakeID: take, Start: 1, End: 2}}})
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, domain.StateEditing, f.state(c)) }, 10*time.Second, 10*time.Millisecond, "back to EDITING")
	view, err := f.svc.Editor.View(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, judged.VersionNumber+1, view.Timeline.VersionNumber)
	require.Len(t, view.Timeline.Clips, 2)
	assert.Equal(t, newShot, view.Timeline.Clips[1].ShotID)
}

// A Branch that hangs from an additional Shot of the trunk inherits it. Its Rough Cut treats it as an
// additional Shot all the same: while its round is open it is not a story Shot that needs a Selected
// Take (no 422 for the whole cut), and once applied it follows its anchor with the kind of its request
// (#326).
func TestRoughCut_ABranchTreatsAnInheritedAdditionalShotAsAdditional(t *testing.T) {
	f, _ := editedFixture(t)
	ctx := context.Background()
	newShot := f.additionalShot(t)
	f.execSQL(t, `UPDATE project SET mode = 'director' WHERE id = ?`, f.projectID)
	branch, err := f.svc.Branch.Create(ctx, newShot, service.BranchInput{})
	require.NoError(t, err)
	branchID := branch.Branch.ID
	f.execSQL(t, `UPDATE project SET state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)

	// The round of the trunk is still open: the additional Shot has no Selected Take.
	view, err := f.svc.Editor.RoughCut(ctx, f.projectID, branchID)
	require.NoError(t, err, "an open round of the series it hangs from does not refuse the Branch's Rough Cut")
	require.Len(t, view.Timeline.Clips, 1)
	assert.Equal(t, f.shotID, view.Timeline.Clips[0].ShotID)

	// The round is applied on the trunk: the Branch's Rough Cut places the Shot after its anchor, as a reaction.
	f.selectTakeOf(t, newShot)
	f.waitCount(t, 10*time.Second, 1, `SELECT COUNT(*) FROM reshoot_request WHERE status = 'applied'`)
	f.waitEditingWorkerIdle(t) // the worker that applied the round holds the stage until it is done
	f.execSQL(t, `UPDATE project SET state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
	view, err = f.svc.Editor.RoughCut(ctx, f.projectID, branchID)
	require.NoError(t, err)
	require.Len(t, view.Timeline.Clips, 2)
	assert.Equal(t, f.shotID, view.Timeline.Clips[0].ShotID)
	assert.Equal(t, newShot, view.Timeline.Clips[1].ShotID)
	assert.Equal(t, domain.ClipReaction, view.Timeline.Clips[1].Kind)
}

func TestEditorReshootLoop_StartFailureDoesNotLeaveAPendingRound(t *testing.T) {
	f := selectedFixture(t, func(string) string { return additionalSpec }, nil)
	ctx := context.Background()
	view, err := f.svc.Editor.RoughCut(ctx, f.projectID, "")
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	timelineID := view.Timeline.ID
	f.llm.editor = needReaction

	tiny := 0.01
	f.setBudget(t, nil, &tiny)
	_, err = f.svc.Editor.JudgeTimeline(ctx, timelineID)
	require.ErrorIs(t, err, service.ErrBudgetExceeded)
	assert.NotContains(t, err.Error(), "まだできていません")
	assert.Equal(t, domain.StateEditJudging, f.state(t))
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM reshoot_request WHERE status = 'requested'`))
	assert.Equal(t, 1, f.trunkShots(t), "a Shot whose generation never started is not left in the Project")
	f.setBudget(t, nil, nil)
	_, err = f.svc.Editor.RequestAdditionalShot(ctx, timelineID)
	require.NoError(t, err, "the same timeline can ask again after the start failure")
	assert.Equal(t, domain.StateGenerating, f.state(t))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM reshoot_request WHERE project_id = ? AND status = 'requested'`, f.projectID))
}

// On a Branch whose anchor Shot lies in the trunk the additional Shot gets a Scene of its own. When
// no Shot can start, that Scene is removed with it: asking again must not pile up empty Scenes.
func TestEditorReshootLoop_StartFailureOnABranchLeavesNoEmptyScene(t *testing.T) {
	f, branchID, timelineID := branchFixture(t, func(string) string { return additionalSpec })
	ctx := context.Background()
	f.llm.editor = needReaction

	tiny := 0.01
	f.setBudget(t, nil, &tiny)
	_, err := f.svc.Editor.JudgeTimeline(ctx, timelineID)
	require.ErrorIs(t, err, service.ErrBudgetExceeded)
	_, err = f.svc.Editor.RequestAdditionalShot(ctx, timelineID)
	require.ErrorIs(t, err, service.ErrBudgetExceeded)
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM scene WHERE branch_id = ?`, branchID), "the Scene made for the failed additional Shot is removed")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM shot`))

	f.setBudget(t, nil, nil)
	_, err = f.svc.Editor.RequestAdditionalShot(ctx, timelineID)
	require.NoError(t, err)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM scene WHERE branch_id = ?`, branchID), "the successful retry makes exactly one Scene")
}

// When only some of the additional Shots can start (here the second one's spec breaks a LOCK Brand
// constraint),
// the one that started belongs to the round and the one that did not is removed: no Shot is left
// outside a round, and asking again is refused instead of making the started Shot a second time.
func TestEditorReshootLoop_PartialStartKeepsTheStartedShotInTheRound(t *testing.T) {
	f, judged := editedFixtureJudging(t, needTwoShots("驚いて振り返る", "BROKEN-PLAN"), brokenPlanner, func(f genFixture) {
		_, err := f.svc.Brand.Add(context.Background(), f.projectID, service.BrandForm{Type: string(domain.BrandForbidden), Text: "BANNED"})
		require.NoError(t, err)
	})
	ctx := context.Background()
	assert.Equal(t, 2, f.trunkShots(t), "the Shot that started stays, the one that did not is removed")
	assert.Equal(t, domain.StateGenerating, f.state(t))
	var started string
	require.NoError(t, f.query(t, `SELECT id FROM shot WHERE id <> ?`, f.shotID).Scan(&started))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM reshoot_request WHERE shot_id = ? AND status = 'requested'`, started), "the started Shot is part of the round")
	_, err := f.svc.Editor.RequestAdditionalShot(ctx, judged.ID)
	require.ErrorIs(t, err, service.ErrConflict)
	assert.Equal(t, 2, f.trunkShots(t), "asking again does not make the started Shot a second time")
}

// On a Branch whose anchor Shot lies in the trunk the additional Shots share one Scene of the Branch.
// When only some of them can start, the Scene stays with the one that did: it must not be removed
// together with the Shot that failed (#333).
func TestEditorReshootLoop_PartialStartOnABranchKeepsTheSceneOfTheStartedShot(t *testing.T) {
	f, branchID, timelineID := branchFixture(t, brokenPlanner)
	ctx := context.Background()
	_, err := f.svc.Brand.Add(ctx, f.projectID, service.BrandForm{Type: string(domain.BrandForbidden), Text: "BANNED"})
	require.NoError(t, err)
	f.llm.editor = needTwoShots("驚いて振り返る", "BROKEN-PLAN")
	_, err = f.svc.Editor.JudgeTimeline(ctx, timelineID)
	require.NoError(t, err)

	var started string
	require.NoError(t, f.query(t, `SELECT shot_id FROM reshoot_request WHERE status = 'requested'`).Scan(&started), "the Shot that started is in the round")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM reshoot_request`))
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE sh.id = ? AND s.branch_id = ?`, started, branchID), "the started Shot still belongs to a Scene of the Branch")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM scene WHERE branch_id = ?`, branchID))
	assert.Equal(t, 2, f.count(t, `SELECT COUNT(*) FROM shot`), "the Shot that could not start is gone")
}

// When adding the second additional Shot fails (a trigger aborts the insert), the first one, which
// belongs to no round, is not left behind with its Branch Scene (#572).
func TestEditorReshootLoop_AddFailureRemovesTheShotsAddedBeforeIt(t *testing.T) {
	f, branchID, timelineID := branchFixture(t, func(string) string { return additionalSpec })
	f.llm.editor = needTwoShots("驚いて振り返る", "手元のアップ")
	// The Project has one Shot: the first additional Shot is the 2nd, the second one is refused.
	f.execSQL(t, `CREATE TRIGGER refuse_third_shot BEFORE INSERT ON shot WHEN (SELECT COUNT(*) FROM shot) >= 2 BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	_, err := f.svc.Editor.JudgeTimeline(context.Background(), timelineID)
	require.Error(t, err)
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM shot`), "the Shot added before the failure is removed")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM scene WHERE branch_id = ?`, branchID), "so is the Branch Scene made for it")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM reshoot_request`))
	assert.Equal(t, domain.StateEditJudging, f.state(t))
}

// The request going away while the additional Shots start (the client left, AUTO is shutting down)
// must not leave a generation that has started outside a round, nor the Shot that could not start
// in the Project: the bookkeeping after the loop does not depend on the request.
func TestEditorReshootLoop_ACancelledRequestStillRecordsTheRoundOfTheStartedShots(t *testing.T) {
	twoNeeds := needTwoShots("FIRST-SHOT", "SECOND-SHOT")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := selectedFixture(t, func(user string) string {
		if strings.Contains(user, "SECOND-SHOT") {
			cancel() // the client leaves while the second generation starts, after the first one did
		}
		return additionalSpec
	}, nil)
	view, err := f.svc.Editor.RoughCut(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	f.llm.editor = twoNeeds

	_, _ = f.svc.Editor.JudgeTimeline(ctx, view.Timeline.ID)
	require.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM reshoot_request WHERE status = 'requested'`), "the Shot that started is in a round")
	assert.Equal(t, 2, f.trunkShots(t), "the Shot that could not start is removed")
	assert.Equal(t, domain.StateGenerating, f.state(t))
}

// The Selected Take of a Shot whose Editing is fixed changes: the Rough Cut made again plays the
// new Take with the user's cut, so that the Editor Judge's "the Take changed" finding goes away.
func TestRoughCut_AFixedShotFollowsItsNewSelectedTake(t *testing.T) {
	ctx := context.Background()
	f := selectedFixture(t, nil, nil)
	view, err := f.svc.Editor.RoughCut(ctx, f.projectID, "")
	require.NoError(t, err)
	oldTake := view.Timeline.Clips[0].TakeID
	f.execSQL(t, `INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l-edit', ?, 'shot', ?, 'editing', 'lock')`, f.projectID, f.shotID)
	var newTake string
	require.NoError(t, f.query(t, `SELECT t.id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ? AND t.id <> ? ORDER BY t.rowid LIMIT 1`, f.shotID, oldTake).Scan(&newTake))
	_, err = f.svc.Takes.Select(ctx, newTake)
	require.NoError(t, err)
	f.waitEditingWorkerIdle(t) // the Select woke the worker, which may hold the stage

	again, err := f.svc.Editor.RoughCut(ctx, f.projectID, "")
	require.NoError(t, err)
	require.Len(t, again.Timeline.Clips, 1)
	assert.Equal(t, newTake, again.Timeline.Clips[0].TakeID)
	f.llm.editor = `{"verdict":"pass","findings":[]}`
	judged, err := f.svc.Editor.JudgeTimeline(ctx, again.Timeline.ID)
	require.NoError(t, err)
	require.NotNil(t, judged.Timeline.Judge)
	for _, finding := range judged.Timeline.Judge.Findings {
		assert.NotContains(t, finding.Issue, "採用 Take が編集後に変わっています")
	}
}
