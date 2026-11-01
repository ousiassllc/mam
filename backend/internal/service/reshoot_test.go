package service_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
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
	return editedFixtureWith(t, judge, planner, before, nil)
}

// selectedFixture is an AUTO Project whose only Shot has a Selected Take and which is about to get its
// Rough Cut (state VISUAL_DESIGN, drama template).
func selectedFixture(t *testing.T, planner func(string) string, tweak func(*service.Deps)) genFixture {
	t.Helper()
	f := newGenFixtureWithDeps(t, true, func(string) (uint64, error) { return 1 << 40, nil }, tweak)
	f.llm.extractor = extractAnswer
	f.llm.planner = planner
	f.buildReferencePack(t)
	f.generateAndWait(t, f.shotID, 2)
	f.selectTakeOf(t, f.shotID)
	f.waitEditingWorkerIdle(t) // the Select woke the editing worker: let it end before the state below starts the AUTO chain
	f.execSQL(t, `UPDATE project SET production_template = 'drama', state = 'VISUAL_DESIGN' WHERE id = ?`, f.projectID)
	return f
}

// editedFixtureWith is editedFixtureJudging with a last look at the Deps (nil: none).
func editedFixtureWith(t *testing.T, judge string, planner func(string) string, before func(genFixture), tweak func(*service.Deps)) (genFixture, domain.EditTimelineVersion) {
	t.Helper()
	f := selectedFixture(t, planner, tweak)
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

// waitEditingWorkerIdle waits until the worker behind ResumeEditing (woken by a Take selection or a
// finished job) is idle: it holds the editing stage while it runs, so a stage called meanwhile fails with ErrStageBusy (#608).
func (f genFixture) waitEditingWorkerIdle(t *testing.T) {
	t.Helper()
	require.Eventually(t, f.svc.Editor.EditingWorkerIdle, 15*time.Second, 5*time.Millisecond, "the editing worker goes back to waiting")
}

// additionalShot waits until the Director has made the additional Shot and it has a Take.
func (f genFixture) additionalShot(t *testing.T) string {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 2, f.trunkShots(c)) }, 10*time.Second, 10*time.Millisecond, "the Director makes the additional Shot")
	f.waitJobsDone(t)
	var newShot string
	require.NoError(t, f.query(t, `SELECT id FROM shot WHERE id <> ?`, f.shotID).Scan(&newShot))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Positive(c, f.count(c, `SELECT COUNT(*) FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE pv.shot_id = ?`, newShot))
	}, 30*time.Second, 10*time.Millisecond)
	return newShot
}

// needTwoShots is the verdict of an Editor Judge that finds a reaction and an insert Shot missing.
func needTwoShots(reaction, insert string) string {
	return `{"verdict":"need_shot","findings":[` +
		`{"clip_index":1,"issue":"a","action":"a","need_shot":{"kind":"reaction","after_clip":1,"objective":"` + reaction + `","duration_seconds":2}},` +
		`{"clip_index":1,"issue":"b","action":"b","need_shot":{"kind":"insert","after_clip":1,"objective":"` + insert + `","duration_seconds":2}}]}`
}

// brokenPlanner answers a BROKEN-PLAN Shot with a spec that breaks the BANNED Brand constraint.
func brokenPlanner(user string) string {
	if strings.Contains(user, "BROKEN-PLAN") {
		return strings.Replace(additionalSpec, "Character looks back, surprised", "a BANNED word", 1)
	}
	return additionalSpec
}

// branchFixture is a Project with a Branch whose Rough Cut is made (all additional Shots go into one Scene of it).
func branchFixture(t *testing.T, planner func(string) string) (f genFixture, branchID, timelineID string) {
	t.Helper()
	f = selectedFixture(t, planner, nil)
	f.llm.director = directorFor(sameSceneAnswer)
	branchID = f.branch(t, service.BranchInput{}).Branch.ID
	view, err := f.svc.Editor.RoughCut(context.Background(), f.projectID, branchID)
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	return f, branchID, view.Timeline.ID
}

func TestEditorReshootLoop_AdditionalShotIsMadeAndPutIntoTheTimelineAfterItsAnchor(t *testing.T) {
	f, judged := editedFixture(t)
	ctx := context.Background()

	// The NEED_SHOT verdict in AUTO asks the Director by itself: a new Shot is generated.
	newShot := f.additionalShot(t)
	assert.Equal(t, domain.StateGenerating, f.state(t))
	f.execSQL(t, `UPDATE project SET mode = 'director' WHERE id = ?`, f.projectID) // the AUTO Judge of the round is tested below

	view, err := f.svc.Editor.View(ctx, f.projectID, "")
	require.NoError(t, err)
	assert.Equal(t, 1, view.PendingShots, "the Shot has no Selected Take yet")

	// Selecting its Take returns to the Editor: the clip follows its anchor as a reaction.
	f.selectTakeOf(t, newShot)
	require.EventuallyWithT(t, func(c *assert.CollectT) { // EDITING comes before the round is saved: wait for the version with the Shot
		v, err := f.svc.Editor.View(ctx, f.projectID, "")
		require.NoError(c, err)
		require.NotNil(c, v.Timeline)
		assert.True(c, v.Project.State == domain.StateEditing && v.PendingShots == 0 && v.Timeline.VersionNumber == judged.VersionNumber+1)
		view = v
	}, 10*time.Second, 10*time.Millisecond, "back to EDITING with the Shot in the timeline")
	assert.Equal(t, judged.VersionNumber+1, view.Timeline.VersionNumber)
	require.Len(t, view.Timeline.Clips, 2)
	assert.Equal(t, f.shotID, view.Timeline.Clips[0].ShotID)
	assert.Equal(t, newShot, view.Timeline.Clips[1].ShotID)
	assert.Equal(t, domain.ClipReaction, view.Timeline.Clips[1].Kind)
	assert.InDelta(t, 2, view.Timeline.Clips[1].Length(), 1e-9)
	assert.Zero(t, view.PendingShots)

	// A Rough Cut made again keeps the additional Shot (it is not a story Shot).
	f.waitEditingWorkerIdle(t) // the worker that put the Shot in holds the stage until it has looked at the Judge too
	again, err := f.svc.Editor.RoughCut(ctx, f.projectID, "")
	require.NoError(t, err)
	require.Len(t, again.Timeline.Clips, 2)
	assert.Equal(t, domain.ClipReaction, again.Timeline.Clips[1].Kind)
}

func TestEditorReshootLoop_AFinishedShotWaitsForTheBusyStageAndIsPutInWhenItEnds(t *testing.T) {
	f, _ := editedFixture(t)
	newShot := f.additionalShot(t)
	f.execSQL(t, `UPDATE project SET mode = 'director' WHERE id = ?`, f.projectID) // the AUTO Judge of the round is tested below

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
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, domain.StateEditing, f.state(c)) }, 10*time.Second, 10*time.Millisecond, "back to EDITING after the stage ended")
	view, err := f.svc.Editor.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotNil(t, view.Timeline)
	require.Len(t, view.Timeline.Clips, 2)
	assert.Equal(t, newShot, view.Timeline.Clips[1].ShotID)
}

func TestEditorReshootLoop_StopsAtTheRetryPolicyLimitAndEscalates(t *testing.T) {
	f, _ := editedFixture(t)
	ctx := context.Background()
	newShot := f.additionalShot(t)

	// One round is the whole budget of Retry Policy "max retries = 1": the Judge that follows the
	// round (it still finds a Shot missing) does not ask a second one.
	_, err := f.svc.Policy.SetQualityPolicy(ctx, f.projectID, domain.QualityBalanced, &service.PolicyForm{MaxRetries: "1"})
	require.NoError(t, err)
	f.selectTakeOf(t, newShot)
	f.waitCount(t, 10*time.Second, 1, `SELECT COUNT(*) FROM chat_message WHERE kind = 'escalation'`)
	f.waitEditingWorkerIdle(t)
	assert.Equal(t, domain.StateAwaitingApproval, f.state(t), "no second round: the Project waits for the user's decision")
	assert.Equal(t, 2, f.trunkShots(t), "no further Shot was made")
	view, err := f.svc.Editor.View(ctx, f.projectID, "")
	require.NoError(t, err)
	_, err = f.svc.Editor.RequestAdditionalShot(ctx, view.Timeline.ID)
	assert.ErrorIs(t, err, service.ErrConflict, "the escalation has to be answered first")
	assert.Equal(t, 2, f.trunkShots(t))
}

// reshotFixture is an AUTO Project whose additional Shot is made and whose Take is about to be
// selected; the Editor Judge answers answer from then on.
func reshotFixture(t *testing.T, answer string) (genFixture, string) {
	t.Helper()
	f, _ := editedFixture(t)
	newShot := f.additionalShot(t)
	f.llm.editor = answer
	return f, newShot
}

func TestEditorReshootLoop_AutoJudgesTheTimelineWithTheAdditionalShotsAndGoesOnOnPass(t *testing.T) {
	f, newShot := reshotFixture(t, `{"verdict":"pass","findings":[]}`)

	f.selectTakeOf(t, newShot)
	// JUDGING, EDITING and EDIT_JUDGING come first: wait for the second Judge call and the worker's end.
	require.Eventually(t, func() bool { return f.llm.calls(llm.AgentEditor) == 2 }, 10*time.Second, 10*time.Millisecond, "the Judge ran by itself")
	f.waitEditingWorkerIdle(t)
	assert.Equal(t, 2, f.llm.calls(llm.AgentEditor), "once for the Rough Cut, once for the version with the additional Shot")
	assert.Equal(t, 2, f.trunkShots(t))
	view, err := f.svc.Editor.View(context.Background(), f.projectID, "")
	require.NoError(t, err)
	require.NotNil(t, view.Timeline.Judge)
	assert.Equal(t, domain.EditorPass, view.Timeline.Judge.Verdict)
	assert.Equal(t, view.Timeline.ID, view.Timeline.Judge.EditTimelineVersionID, "the Judge looked at the latest version")
}

func TestEditorReshootLoop_AutoStopsAtReEditAfterTheAdditionalShotsAreJudged(t *testing.T) {
	f, newShot := reshotFixture(t, `{"verdict":"re_edit","findings":[{"clip_index":1,"issue":"長い","action":"短くする"}]}`)
	f.selectTakeOf(t, newShot)
	require.Eventually(t, func() bool { return f.llm.calls(llm.AgentEditor) == 2 }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 2, f.llm.calls(llm.AgentEditor), "Re-edit is the user's: the Judge does not run again")
	assert.Equal(t, domain.StateEditing, f.state(t))
}

func TestEditorReshootLoop_AutoAsksTheNextRoundWhenTheJudgeStillFindsAShotMissing(t *testing.T) {
	f, newShot := reshotFixture(t, needReaction)

	f.selectTakeOf(t, newShot)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, 3, f.trunkShots(c)) }, 10*time.Second, 10*time.Millisecond, "the second round was asked by itself")
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, domain.StateGenerating, f.state(c)) }, 10*time.Second, 10*time.Millisecond)
}

func TestEditorReshootLoop_DirectorProjectIsNotJudgedAutomatically(t *testing.T) {
	f, newShot := reshotFixture(t, `{"verdict":"pass","findings":[]}`)
	f.execSQL(t, `UPDATE project SET mode = 'director' WHERE id = ?`, f.projectID)
	f.selectTakeOf(t, newShot)
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, domain.StateEditing, f.state(c)) }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 1, f.llm.calls(llm.AgentEditor), "only the Judge of the Rough Cut ran")
	assert.Equal(t, domain.StateEditing, f.state(t))
}

// A Project that went on to READY / EXPORTED with a round still open (the user edited the timeline
// meanwhile) is not touched when the round's last Take is selected: a new Edit Timeline version would
// leave the Audio Timeline and the export on the old edit. The round waits until the Project is
// edited again.
func TestEditorReshootLoop_AReadyOrExportedProjectKeepsItsEditTimeline(t *testing.T) {
	for _, state := range []domain.ProjectState{domain.StateReady, domain.StateExported, domain.StateAwaitingApproval} {
		t.Run(string(state), func(t *testing.T) {
			f, judged := editedFixture(t)
			ctx := context.Background()
			newShot := f.additionalShot(t)
			f.execSQL(t, `UPDATE project SET mode = 'director' WHERE id = ?`, f.projectID)
			f.execSQL(t, `UPDATE project SET state = ? WHERE id = ?`, string(state), f.projectID)

			f.selectTakeOf(t, newShot)
			time.Sleep(300 * time.Millisecond)
			assert.Equal(t, state, f.state(t))
			view, err := f.svc.Editor.View(ctx, f.projectID, "")
			require.NoError(t, err)
			assert.Equal(t, judged.VersionNumber, view.Timeline.VersionNumber, "no new version is saved")
			assert.Equal(t, 1, f.count(t, `SELECT COUNT(*) FROM reshoot_request WHERE status = 'requested'`), "the round stays open")

			f.execSQL(t, `UPDATE project SET state = 'EDITING' WHERE id = ?`, f.projectID)
			f.svc.Editor.ResumeEditing()
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				v, err := f.svc.Editor.View(ctx, f.projectID, "")
				require.NoError(c, err)
				require.NotNil(c, v.Timeline)
				assert.Equal(c, judged.VersionNumber+1, v.Timeline.VersionNumber)
			}, 10*time.Second, 10*time.Millisecond, "the round goes into the timeline once the Project is edited again")
			assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM reshoot_request WHERE status = 'requested'`))
		})
	}
}
