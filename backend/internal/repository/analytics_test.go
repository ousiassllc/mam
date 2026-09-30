package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// judgedRaw writes a Judge result of a Take with the Issues JSON.
func judgedRaw(t *testing.T, f genRepoFixture, takeID, verdict, issues, at string) {
	t.Helper()
	require.NoError(t, exec(t, f.db,
		`INSERT INTO judge_result (id, take_id, result, issues, created_at) VALUES (?, ?, ?, ?, ?)`, domain.NewID(), takeID, verdict, issues, at))
}

func setTakeTimes(t *testing.T, f genRepoFixture, takeID, at string) {
	t.Helper()
	require.NoError(t, exec(t, f.db, `UPDATE take SET created_at = ? WHERE id = ?`, at, takeID))
}

// analyticsFixture is one finished Project of two Shots: Shot 1 passed at the first Take; Shot 2's
// first Take failed on the Character and a repair made the Take that passed. The user selected the
// second Take of Shot 2 against the AI Pick and edited a Shot Spec by hand; the Project was exported.
func analyticsFixture(t *testing.T) (genRepoFixture, *repository.AnalyticsRepository) {
	t.Helper()
	ctx := context.Background()
	f := newGenRepoFixture(t)
	shots, err := f.repo.ListProjectShots(ctx, f.projectID)
	require.NoError(t, err)
	a := repository.NewAnalyticsRepository(f.db)

	one := newTake(t, f, shots[0].Shot.ID, "one.mp4", 10, 1, nil)
	setTakeTimes(t, f, one, "2026-01-01T00:10:00.000Z")
	judgedRaw(t, f, one, "pass", `[{"type":"x","severity":"low","dimension":"prompt_adherence"}]`, "2026-01-01T00:11:00.000Z")
	require.NoError(t, exec(t, f.db, `UPDATE shot SET selected_take_id = ? WHERE id = ?`, one, shots[0].Shot.ID))

	two1 := newTake(t, f, shots[1].Shot.ID, "two1.mp4", 10, 2, nil)
	setTakeTimes(t, f, two1, "2026-01-01T00:12:00.000Z")
	judgedRaw(t, f, two1, "fail", `[{"type":"drift","severity":"high","dimension":"character_identity"},{"type":"p","severity":"medium","dimension":"prompt_adherence"}]`, "2026-01-01T00:13:00.000Z")
	two2 := newTake(t, f, shots[1].Shot.ID, "two2.mp4", 10, 3, nil)
	setTakeTimes(t, f, two2, "2026-01-01T00:14:00.000Z")
	judgedRaw(t, f, two2, "pass", `[]`, "2026-01-01T00:15:00.000Z")
	// A later Judge run of the failed Take that passes never changes what its first run said.
	var judgeID string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT id FROM judge_result WHERE take_id = ?`, two1).Scan(&judgeID))
	require.NoError(t, exec(t, f.db, `INSERT INTO repair_action (id, judge_result_id, action_type, resulting_take_id) VALUES ('ra', ?, 'x', ?)`, judgeID, two2))
	require.NoError(t, exec(t, f.db, `UPDATE shot SET selected_take_id = ? WHERE id = ?`, two2, shots[1].Shot.ID))
	require.NoError(t, exec(t, f.db, `UPDATE take SET status = 'ai_pick' WHERE id = ?`, two1))

	require.NoError(t, a.Record(ctx, domain.Intervention{TakeID: two2, Kind: domain.InterventionSelectTake, Override: true}))
	require.NoError(t, a.Record(ctx, domain.Intervention{ShotID: shots[1].Shot.ID, Kind: domain.InterventionEditShotSpec, Override: true}))
	require.NoError(t, a.Record(ctx, domain.Intervention{ProjectID: f.projectID, Kind: domain.InterventionSwitchToDirector}))

	require.NoError(t, exec(t, f.db, `UPDATE project SET created_at = '2026-01-01T00:00:00.000Z', state = 'EXPORTED' WHERE id = ?`, f.projectID))
	require.NoError(t, exec(t, f.db, `INSERT INTO edit_timeline_version (id, project_id, version_number, timeline, created_at) VALUES ('e1', ?, 1, '{}', '2026-01-01T00:20:00.000Z')`, f.projectID))
	require.NoError(t, exec(t, f.db, `INSERT INTO edit_timeline_version (id, project_id, version_number, timeline, created_at) VALUES ('e2', ?, 2, '{}', '2026-01-01T00:40:00.000Z')`, f.projectID))
	require.NoError(t, exec(t, f.db, `INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec, exported_at) VALUES ('x1', ?, 'f.mp4', '1080p', '16:9', 'h264', '2026-01-01T01:00:00.000Z')`, f.projectID))
	return f, a
}

func TestAnalyticsRepository_MetricsOfAFinishedProject(t *testing.T) {
	f, a := analyticsFixture(t)

	m, err := a.Metrics(context.Background(), f.projectID)
	require.NoError(t, err)

	assert.Equal(t, 2, m.Shots)
	assert.Equal(t, domain.Ratio{Numerator: 1, Denominator: 2}, m.FirstPassShotAcceptance, "only Shot 1 passed at its first Take")
	assert.Equal(t, domain.Ratio{Numerator: 2, Denominator: 2}, m.FinalShotAcceptance)
	assert.Equal(t, domain.Ratio{Numerator: 1, Denominator: 2}, m.AverageRetakesPerShot, "one repair Take over two Shots")
	assert.Equal(t, domain.Ratio{Numerator: 1, Denominator: 2}, m.UserOverride, "two overrides on Shot 2 count once")
	assert.Equal(t, 2, m.OverrideEvents)
	assert.Equal(t, domain.Ratio{Numerator: 2, Denominator: 1}, m.InterventionsPerFinished, "the takeover is not a generation operation")

	assert.InDelta(t, 6, m.CostPerAcceptedSecond.Numerator, 1e-9, "every Take costs, accepted or not: 1 + 2 + 3")
	assert.InDelta(t, 5, m.CostPerAcceptedSecond.Denominator, 1e-9, "the selected Takes have no length of their own: the Shots' 3 s + 2 s")

	assert.Equal(t, domain.Ratio{Numerator: 1, Denominator: 3}, m.CharacterContinuityFail)
	assert.Equal(t, domain.Ratio{Numerator: 0, Denominator: 3}, m.ProductAccuracyFail)
	assert.Equal(t, domain.Ratio{Numerator: 2, Denominator: 3}, m.PromptAdherence, "a low Issue is no failure, a medium one is")

	assert.Equal(t, domain.Ratio{Numerator: 0, Denominator: 1}, m.AutoCompletion, "started in AUTO but taken over")
	assert.Equal(t, domain.Ratio{Numerator: 0, Denominator: 1}, m.NorthStar)
	assert.Equal(t, domain.Ratio{Numerator: 1, Denominator: 1}, m.ExportCompletion)
	assert.Equal(t, domain.Ratio{Numerator: 1200, Denominator: 1}, m.TimeToFirstRoughCut, "the first timeline version, not the edited one")
	assert.Equal(t, domain.Ratio{Numerator: 3600, Denominator: 1}, m.TimeToFinalVideo)
}

func TestAnalyticsRepository_NorthStarCountsOnlyFinishedProjectsWithoutIntervention(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	projects := repository.NewProjectRepository(db)
	a := repository.NewAnalyticsRepository(db)
	untouched := createProject(t, projects, domain.StateReady)
	touched := createProject(t, projects, domain.StateReady)
	unfinished := createProject(t, projects, domain.StateGenerating)
	draft := createProject(t, projects, domain.StateDraft)
	require.NoError(t, a.Record(ctx, domain.Intervention{ProjectID: touched, Kind: domain.InterventionRequestChanges, Override: true}))

	all, err := a.Metrics(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, 4, all.Projects)
	assert.Equal(t, 3, all.Started, "a Project in DRAFT has not started")
	assert.Equal(t, 2, all.Completed)
	assert.Equal(t, domain.Ratio{Numerator: 1, Denominator: 3}, all.NorthStar)
	assert.Equal(t, domain.Ratio{Numerator: 2, Denominator: 3}, all.AutoCompletion)
	assert.Equal(t, domain.Ratio{Numerator: 0, Denominator: 2}, all.ExportCompletion, "READY but not exported")

	one, err := a.Metrics(ctx, untouched)
	require.NoError(t, err)
	assert.Equal(t, domain.Ratio{Numerator: 1, Denominator: 1}, one.NorthStar)
	assert.Equal(t, 1, one.Projects)
	unf, err := a.Metrics(ctx, unfinished)
	require.NoError(t, err)
	assert.Equal(t, domain.Ratio{Numerator: 0, Denominator: 1}, unf.NorthStar)
	dr, err := a.Metrics(ctx, draft)
	require.NoError(t, err)
	assert.False(t, dr.NorthStar.Defined(), "nothing has started, so there is nothing to divide by")
}

func TestAnalyticsRepository_RecordFindsTheProjectAndRefusesUnknownTargets(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	a := repository.NewAnalyticsRepository(f.db)

	require.NoError(t, a.Record(ctx, domain.Intervention{ShotID: f.shotID, Kind: domain.InterventionGenerate}))
	var project string
	require.NoError(t, f.db.Reader().QueryRow(`SELECT project_id FROM human_intervention`).Scan(&project))
	assert.Equal(t, f.projectID, project)

	assert.ErrorIs(t, a.Record(ctx, domain.Intervention{ShotID: "missing", Kind: domain.InterventionGenerate}), repository.ErrNotFound)
	assert.ErrorIs(t, a.Record(ctx, domain.Intervention{TakeID: "missing", Kind: domain.InterventionSelectTake}), repository.ErrNotFound)
	_, err := a.Metrics(ctx, "missing")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestAnalyticsRepository_ShotStateSeesPassedTakesThatAreNotRejected(t *testing.T) {
	ctx := context.Background()
	f := newGenRepoFixture(t)
	a := repository.NewAnalyticsRepository(f.db)

	has, passed, err := a.ShotState(ctx, f.shotID)
	require.NoError(t, err)
	assert.False(t, has)
	assert.False(t, passed)

	take := newTake(t, f, f.shotID, "s.mp4", 1, 1, nil)
	judgedRaw(t, f, take, "pass", `[]`, "2026-01-01T00:00:00.000Z")
	has, passed, err = a.ShotState(ctx, f.shotID)
	require.NoError(t, err)
	assert.True(t, has)
	assert.True(t, passed)

	require.NoError(t, exec(t, f.db, `UPDATE take SET status = 'rejected' WHERE id = ?`, take))
	has, passed, _ = a.ShotState(ctx, f.shotID)
	assert.True(t, has)
	assert.False(t, passed, "a rejected Take no longer counts as the AI's acceptable result")
}
