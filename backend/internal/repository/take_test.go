package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// addTakes adds Takes t2..tN (with video assets a1..aN; t1 comes from seedTake) to the seeded Shot.
func addTakes(t *testing.T, db *repository.DB, n int) {
	t.Helper()
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO asset (id, project_id, type, file_path, mime_type) VALUES ('a1', 'p1', 'video', 'p1/a.mp4', 'video/mp4')`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE take SET model_parameters = '{"resolution":"1080p","duration_seconds":4}', created_at = '2026-01-01T00:00:01.000Z' WHERE id = 't1'`); err != nil {
			return err
		}
		for i := 2; i <= n; i++ {
			if _, err := tx.ExecContext(ctx, `INSERT INTO asset (id, project_id, type, file_path, mime_type) VALUES (?, 'p1', 'video', ?, 'video/mp4')`,
				fmt.Sprintf("a%d", i), fmt.Sprintf("p1/%d.mp4", i)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO take (id, prompt_version_id, model_name, file_path, created_at) VALUES (?, 'pv1', 'seedance-2.0', ?, ?)`,
				fmt.Sprintf("t%d", i), fmt.Sprintf("p1/%d.mp4", i), fmt.Sprintf("2026-01-01T00:00:%02d.000Z", i)); err != nil {
				return err
			}
		}
		return nil
	}))
}

// judged saves a Judge result whose Issues give the Take the scores it needs.
func judged(t *testing.T, db *repository.DB, takeID string, verdict domain.JudgeVerdict, issues ...domain.JudgeIssue) {
	t.Helper()
	_, err := repository.NewJudgeRepository(db).Save(context.Background(), domain.JudgeResult{TakeID: takeID, Result: verdict, Issues: issues})
	require.NoError(t, err)
}

func statuses(t *testing.T, r *repository.TakeRepository) map[string]domain.TakeStatus {
	t.Helper()
	takes, err := r.ShotTakes(context.Background(), "sh1")
	require.NoError(t, err)
	out := map[string]domain.TakeStatus{}
	for _, tk := range takes {
		out[tk.ID] = tk.Status
	}
	return out
}

func selectedTake(t *testing.T, db *repository.DB) string {
	t.Helper()
	var id sql.NullString
	require.NoError(t, db.Reader().QueryRow(`SELECT selected_take_id FROM shot WHERE id = 'sh1'`).Scan(&id))
	return id.String
}

func TestTakeRepository_ShotTakesReturnsScoresVerdictAssetAndWhatTheModelReported(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 2)
	r := repository.NewTakeRepository(db)
	judged(t, db, "t1", domain.JudgeFail, domain.JudgeIssue{Type: "logo", Severity: domain.SeverityHigh, Dimension: domain.DimProductAccuracy})

	takes, err := r.ShotTakes(context.Background(), "sh1")

	require.NoError(t, err)
	require.Len(t, takes, 2)
	assert.Equal(t, "t1", takes[0].ID, "oldest first")
	assert.Equal(t, &domain.TakeScores{Product: 65, Acting: 100, Camera: 100, Continuity: 100}, takes[0].Scores)
	assert.Equal(t, domain.JudgeFail, takes[0].Verdict)
	assert.Equal(t, "a1", takes[0].AssetID)
	assert.Equal(t, "1080p", takes[0].Resolution)
	assert.Equal(t, 4.0, takes[0].DurationSeconds)
	assert.Nil(t, takes[1].Scores, "not judged yet")
	assert.Empty(t, takes[1].Verdict)
}

func TestTakeRepository_SelectMakesTheTakeTheShotsAndDemotesThePreviousSelection(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 3)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()

	shot, err := r.Select(ctx, "t2")
	require.NoError(t, err)
	assert.Equal(t, "sh1", shot)
	_, err = r.Select(ctx, "t3")
	require.NoError(t, err)

	assert.Equal(t, map[string]domain.TakeStatus{"t1": domain.TakeCandidate, "t2": domain.TakeCandidate, "t3": domain.TakeSelected}, statuses(t, r))
	assert.Equal(t, "t3", selectedTake(t, db))
	got, err := r.Get(ctx, "t3")
	require.NoError(t, err)
	assert.True(t, got.Adopted)
	assert.Equal(t, "p1", got.ProjectID)
}

func TestTakeRepository_SelectRefusesUnknownTakesAndTakesWhoseFileWasArchived(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 2)
	r := repository.NewTakeRepository(db)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE take SET file_path = NULL, status = 'rejected' WHERE id = 't2'`)
		return err
	}))

	_, missing := r.Select(context.Background(), "nope")
	_, archived := r.Select(context.Background(), "t2")

	assert.ErrorIs(t, missing, repository.ErrNotFound)
	assert.ErrorIs(t, archived, repository.ErrConflict)
	assert.Empty(t, selectedTake(t, db))
}

func TestTakeRepository_RejectExcludesTheTakeAndClearsTheShotsSelectionWhenItWasSelected(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 2)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()
	_, err := r.Select(ctx, "t1")
	require.NoError(t, err)

	_, err = r.Reject(ctx, "t2")
	require.NoError(t, err)
	assert.Equal(t, "t1", selectedTake(t, db), "rejecting another Take keeps the selection")
	_, err = r.Reject(ctx, "t1")
	require.NoError(t, err)

	assert.Equal(t, map[string]domain.TakeStatus{"t1": domain.TakeRejected, "t2": domain.TakeRejected}, statuses(t, r))
	assert.Empty(t, selectedTake(t, db))
	_, err = r.Reject(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestTakeRepository_PickBestInAutoAdoptsThePassedTakeAndMovesThePickWhenABetterOneComesIn(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 3)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()

	none, err := r.PickBest(ctx, "sh1", true)
	require.NoError(t, err)
	assert.Empty(t, none.TakeID, "nothing judged yet")

	judged(t, db, "t1", domain.JudgePass, domain.JudgeIssue{Type: "x", Severity: domain.SeverityMedium, Dimension: domain.DimActing})
	first, err := r.PickBest(ctx, "sh1", true)
	require.NoError(t, err)
	assert.Equal(t, repository.AIPick{TakeID: "t1", Passed: true, Changed: true, Adopted: true}, first)
	again, err := r.PickBest(ctx, "sh1", true)
	require.NoError(t, err)
	assert.False(t, again.Changed, "the same pick is not a change")

	judged(t, db, "t2", domain.JudgePass)
	second, err := r.PickBest(ctx, "sh1", true)
	require.NoError(t, err)

	assert.True(t, second.Changed)
	assert.Equal(t, "t2", second.TakeID)
	assert.Equal(t, map[string]domain.TakeStatus{"t1": domain.TakeCandidate, "t2": domain.TakeAIPick, "t3": domain.TakeCandidate}, statuses(t, r))
	assert.Equal(t, "t2", selectedTake(t, db), "AUTO uses the AI Pick")
}

func TestTakeRepository_PickBestInDirectorRecommendsWithoutAdoptingAndNeverAdoptsAFailedTake(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 2)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()
	judged(t, db, "t1", domain.JudgePass)

	director, err := r.PickBest(ctx, "sh1", false)
	require.NoError(t, err)
	assert.Equal(t, "t1", director.TakeID)
	assert.False(t, director.Adopted)
	assert.Empty(t, selectedTake(t, db), "DIRECTOR waits for the user")

	failedDB := openTestDB(t)
	seedTake(t, failedDB)
	judged(t, failedDB, "t1", domain.JudgeFail)
	failed, err := repository.NewTakeRepository(failedDB).PickBest(ctx, "sh1", true)
	require.NoError(t, err)
	assert.Equal(t, "t1", failed.TakeID, "a failed Take is still recommended")
	assert.False(t, failed.Passed)
	assert.False(t, failed.Adopted)
	assert.Empty(t, selectedTake(t, failedDB))
}

func TestTakeRepository_PickBestLeavesTheUsersSelectionAlone(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 2)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()
	judged(t, db, "t2", domain.JudgePass)
	_, err := r.Select(ctx, "t1")
	require.NoError(t, err)

	pick, err := r.PickBest(ctx, "sh1", true)

	require.NoError(t, err)
	assert.Empty(t, pick.TakeID)
	assert.Equal(t, "t1", selectedTake(t, db))
	assert.Equal(t, map[string]domain.TakeStatus{"t1": domain.TakeSelected, "t2": domain.TakeCandidate}, statuses(t, r))
}

func TestTakeRepository_CreateCompositeIsTheUsersSelectionAndKeepsTheBaseTakesPromptVersionAndModel(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 2)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()
	_, err := r.Select(ctx, "t2")
	require.NoError(t, err)

	id, err := r.CreateComposite(ctx, repository.NewComposite{
		ProjectID: "p1", ShotID: "sh1", BaseTakeID: "t1",
		Parameters: []byte(`{"kind":"partial_take","resolution":"1080p","duration_seconds":3.75}`),
		Asset:      domain.Asset{ID: "acomp", ProjectID: "p1", Type: domain.AssetVideo, FilePath: "p1/comp.mp4", MimeType: "video/mp4", SizeBytes: 10},
	})

	require.NoError(t, err)
	got, err := r.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.TakeSelected, got.Status)
	assert.Equal(t, "acomp", got.AssetID)
	assert.Equal(t, "kling-3.0", got.ModelName, "the model of the first segment's Take")
	assert.Equal(t, "pv1", got.PromptVersionID)
	assert.Equal(t, 3.75, got.DurationSeconds)
	assert.Equal(t, id, selectedTake(t, db))
	assert.Equal(t, domain.TakeCandidate, statuses(t, r)["t2"], "the earlier selection is demoted")

	_, err = r.CreateComposite(ctx, repository.NewComposite{ProjectID: "p1", ShotID: "sh1", BaseTakeID: "other", Parameters: []byte(`{}`),
		Asset: domain.Asset{ID: "a9", ProjectID: "p1", Type: domain.AssetVideo, FilePath: "p1/9.mp4"}})
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestTakeRepository_CandidateCompositeLeavesTheShotsSelectionAlone(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 2)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()
	_, err := r.Select(ctx, "t2")
	require.NoError(t, err)

	id, err := r.CreateComposite(ctx, repository.NewComposite{
		ProjectID: "p1", ShotID: "sh1", BaseTakeID: "t1", Candidate: true,
		Parameters: []byte(`{"kind":"partial_take","resolution":"1080p","duration_seconds":4}`),
		Asset:      domain.Asset{ID: "acand", ProjectID: "p1", Type: domain.AssetVideo, FilePath: "p1/cand.mp4", MimeType: "video/mp4", SizeBytes: 10},
	})

	require.NoError(t, err)
	assert.Equal(t, "t2", selectedTake(t, db))
	assert.Equal(t, domain.TakeCandidate, statuses(t, r)[id])
	assert.Equal(t, domain.TakeSelected, statuses(t, r)["t2"])
}
