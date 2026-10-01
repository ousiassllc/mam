package service_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

const needReaction = `{"verdict":"need_shot","findings":[{"clip_index":1,"issue":"リアクションがない","action":"リアクションショットを足す",` +
	`"need_shot":{"kind":"reaction","after_clip":1,"objective":"驚いて振り返る","duration_seconds":2}}]}`

// editedFixture is a Project whose only Shot has a Selected Take and a Rough Cut, judged NEED_SHOT.
func editedFixture(t *testing.T) (genFixture, domain.EditTimelineVersion) {
	t.Helper()
	return editedFixtureJudging(t, needReaction, func(string) string { return additionalSpec }, nil)
}

const additionalSpec = `{"shot_objective":"Character looks back, surprised","duration":"2.0s","camera":{"lens":"35mm","framing":"close"},"lighting":{"mood":"cold blue"},"continuity":{"start_from_previous_final_state":true}}`

// editedFixtureJudging is editedFixture with the answer of the Editor Judge and of the Prompt Planner
// chosen by the caller (the Planner answers the first Shot before the Judge runs); before, when set,
// runs just before the Judge.
func editedFixtureJudging(t *testing.T, judge string, planner func(string) string, before func(genFixture)) (genFixture, domain.EditTimelineVersion) {
	t.Helper()
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.llm.planner = planner
	f.buildReferencePack(t)
	f.generateAndWait(t, f.shotID, 2)
	f.selectTakeOf(t, f.shotID)
	f.execSQL(t, `UPDATE project SET production_template = 'drama', state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
	ctx := context.Background()
	view, err := f.svc.Editor.RoughCut(ctx, f.projectID, "")
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	if before != nil {
		before(f)
	}
	f.llm.editor = judge
	view, err = f.svc.Editor.JudgeTimeline(ctx, view.Timeline.ID)
	require.NoError(t, err)
	return f, view.Timeline.EditTimelineVersion
}

func (f genFixture) execSQL(t *testing.T, q string, args ...any) {
	t.Helper()
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// selectTakeOf makes the newest Take of the Shot its Selected Take.
func (f genFixture) selectTakeOf(t *testing.T, shotID string) {
	t.Helper()
	var takeID string
	require.NoError(t, f.query(t, `SELECT t.id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ? ORDER BY t.rowid DESC LIMIT 1`, shotID).Scan(&takeID))
	_, err := f.svc.Takes.Select(context.Background(), takeID)
	require.NoError(t, err)
}

func (f genFixture) state(t *testing.T) domain.ProjectState {
	t.Helper()
	p, err := f.svc.Projects.Get(context.Background(), f.projectID)
	require.NoError(t, err)
	return p.State
}

func TestEditorReshootLoop_AdditionalShotIsMadeAndPutIntoTheTimelineAfterItsAnchor(t *testing.T) {
	f, judged := editedFixture(t)
	ctx := context.Background()

	// The NEED_SHOT verdict in AUTO asks the Director by itself: a new Shot is generated.
	require.Eventually(t, func() bool { return f.trunkShots(t) == 2 }, 10*time.Second, 10*time.Millisecond, "the Director makes the additional Shot")
	assert.Equal(t, domain.StateGenerating, f.state(t))
	var newShot string
	require.NoError(t, f.query(t, `SELECT id FROM shot WHERE id <> ?`, f.shotID).Scan(&newShot))
	f.waitJobsDone(t)
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, newShot) > 0
	}, 30*time.Second, 10*time.Millisecond)

	view, err := f.svc.Editor.View(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, 1, view.PendingShots, "the Shot has no Selected Take yet")

	// Selecting its Take returns to the Editor: the clip follows its anchor as a reaction.
	f.selectTakeOf(t, newShot)
	require.Eventually(t, func() bool { return f.state(t) == domain.StateEditing }, 10*time.Second, 10*time.Millisecond, "back to EDITING")
	view, err = f.svc.Editor.View(ctx, f.projectID, "")
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	assert.Equal(t, judged.VersionNumber+1, view.Timeline.VersionNumber)
	require.Len(t, view.Timeline.Clips, 2)
	assert.Equal(t, f.shotID, view.Timeline.Clips[0].ShotID)
	assert.Equal(t, newShot, view.Timeline.Clips[1].ShotID)
	assert.Equal(t, domain.ClipReaction, view.Timeline.Clips[1].Kind)
	assert.InDelta(t, 2, view.Timeline.Clips[1].Length(), 1e-9)
	assert.Zero(t, view.PendingShots)

	// A Rough Cut made again keeps the additional Shot (it is not a story Shot).
	again, err := f.svc.Editor.RoughCut(ctx, f.projectID, "")
	require.NoError(t, err)
	require.Len(t, again.Timeline.Clips, 2)
	assert.Equal(t, domain.ClipReaction, again.Timeline.Clips[1].Kind)
}

func TestEditorReshootLoop_AFinishedShotWaitsForTheBusyStageAndIsPutInWhenItEnds(t *testing.T) {
	f, _ := editedFixture(t)
	require.Eventually(t, func() bool { return f.trunkShots(t) == 2 }, 10*time.Second, 10*time.Millisecond)
	f.waitJobsDone(t)
	var newShot string
	require.NoError(t, f.query(t, `SELECT id FROM shot WHERE id <> ?`, f.shotID).Scan(&newShot))
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, newShot) > 0
	}, 30*time.Second, 10*time.Millisecond)

	// A round that is not complete yet (its last Take is not selected) must not hold the editing
	// stage: the worker woken by the finished job only looks at it. HoldStage here stands for a
	// stage of the user (Final QC, a preview) and must not meet ErrStageBusy (#139).
	// The last Take is selected while that stage holds the Project.
	release, err := f.svc.Editor.HoldStage(f.projectID)
	require.NoError(t, err)
	f.selectTakeOf(t, newShot)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, domain.StateGenerating, f.state(t), "the busy stage keeps the round waiting")

	// Nothing else will start the Reshoot Loop again: the end of the stage does.
	release()
	require.Eventually(t, func() bool { return f.state(t) == domain.StateEditing }, 10*time.Second, 10*time.Millisecond, "back to EDITING after the stage ended")
	view, err := f.svc.Editor.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	require.Len(t, view.Timeline.Clips, 2)
	assert.Equal(t, newShot, view.Timeline.Clips[1].ShotID)
}

func TestEditorReshootLoop_StopsAtTheRetryPolicyLimitAndEscalates(t *testing.T) {
	f, _ := editedFixture(t)
	ctx := context.Background()
	require.Eventually(t, func() bool { return f.trunkShots(t) == 2 }, 10*time.Second, 10*time.Millisecond)
	f.waitJobsDone(t)
	var newShot string
	require.NoError(t, f.query(t, `SELECT id FROM shot WHERE id <> ?`, f.shotID).Scan(&newShot))
	require.Eventually(t, func() bool {
		return f.count(t, `SELECT COUNT(*) FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, newShot) > 0
	}, 30*time.Second, 10*time.Millisecond)
	f.selectTakeOf(t, newShot)
	require.Eventually(t, func() bool { return f.state(t) == domain.StateEditing }, 10*time.Second, 10*time.Millisecond)

	// One round is the whole budget of Retry Policy "max retries = 1".
	_, err := f.svc.Policy.SetQualityPolicy(ctx, f.projectID, domain.QualityBalanced, &service.PolicyForm{MaxRetries: "1"})
	require.NoError(t, err)
	view, err := f.svc.Editor.View(ctx, f.projectID, "")
	require.NoError(t, err)
	view, err = f.svc.Editor.JudgeTimeline(ctx, view.Timeline.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.StateAwaitingApproval, f.state(t), "no second round: the Project waits for the user's decision")
	assert.Equal(t, 2, f.trunkShots(t), "no further Shot was made")
	assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM chat_message WHERE kind = 'escalation'`))
	_, err = f.svc.Editor.RequestAdditionalShot(ctx, view.Timeline.ID)
	assert.ErrorIs(t, err, service.ErrConflict, "the escalation has to be answered first")
	assert.Equal(t, 2, f.trunkShots(t))
}

// A generation that cannot start (the Shot ceiling cannot buy one Take) must not leave the round
// pending: the same timeline can be asked again once the budget allows it.
func TestEditorReshootLoop_StartFailureDoesNotLeaveAPendingRound(t *testing.T) {
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.llm.planner = func(string) string {
		return `{"shot_objective":"Character looks back, surprised","duration":"2.0s","camera":{"lens":"35mm","framing":"close"},"lighting":{"mood":"cold blue"},"continuity":{"start_from_previous_final_state":true}}`
	}
	f.buildReferencePack(t)
	f.generateAndWait(t, f.shotID, 2)
	f.selectTakeOf(t, f.shotID)
	f.execSQL(t, `UPDATE project SET production_template = 'drama', state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
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
	f := newGenFixture(t, true)
	f.llm.extractor = extractAnswer
	f.llm.planner = func(string) string { return additionalSpec }
	f.llm.director = directorFor(sameSceneAnswer)
	f.buildReferencePack(t)
	f.generateAndWait(t, f.shotID, 2)
	f.selectTakeOf(t, f.shotID)
	f.execSQL(t, `UPDATE project SET production_template = 'drama', state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
	ctx := context.Background()
	branchID := f.branch(t, service.BranchInput{}).Branch.ID
	view, err := f.svc.Editor.RoughCut(ctx, f.projectID, branchID)
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	timelineID := view.Timeline.ID
	f.llm.editor = needReaction

	tiny := 0.01
	f.setBudget(t, nil, &tiny)
	_, err = f.svc.Editor.JudgeTimeline(ctx, timelineID)
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
	const twoNeeds = `{"verdict":"need_shot","findings":[` +
		`{"clip_index":1,"issue":"a","action":"a","need_shot":{"kind":"reaction","after_clip":1,"objective":"驚いて振り返る","duration_seconds":2}},` +
		`{"clip_index":1,"issue":"b","action":"b","need_shot":{"kind":"insert","after_clip":1,"objective":"BROKEN-PLAN","duration_seconds":2}}]}`
	f, judged := editedFixtureJudging(t, twoNeeds, func(user string) string {
		if strings.Contains(user, "BROKEN-PLAN") {
			return strings.Replace(additionalSpec, "Character looks back, surprised", "a BANNED word", 1)
		}
		return additionalSpec
	}, func(f genFixture) {
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
