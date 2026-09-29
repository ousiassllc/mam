package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// seedTake inserts a Project, Scene, Shot, prompt version and Take and returns their IDs.
func seedTake(t *testing.T, db *repository.DB) (projectID, shotID, takeID string) {
	t.Helper()
	projectID, shotID, takeID = "p1", "sh1", "t1"
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{"shot_objective":"run"}')`,
			`INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('t1', 'pv1', 'kling-3.0', 'p1/a.mp4')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	return
}

func TestJudgeRepository_KeepsEveryRunAndLatestReturnsTheNewest(t *testing.T) {
	db := openTestDB(t)
	_, _, take := seedTake(t, db)
	r := repository.NewJudgeRepository(db)
	ctx := context.Background()

	_, err := r.Latest(ctx, take)
	require.ErrorIs(t, err, repository.ErrNotFound, "never judged")

	first, err := r.Save(ctx, domain.JudgeResult{TakeID: take, Result: domain.JudgePass, OverallConfidence: 0.9})
	require.NoError(t, err)
	assert.NotEmpty(t, first.ID)
	second, err := r.Save(ctx, domain.JudgeResult{
		TakeID: take, Result: domain.JudgeFail, OverallConfidence: 0.7,
		Issues:   []domain.JudgeIssue{{Type: "camera_speed", Severity: domain.SeverityMedium, TimeRange: "0.8-1.7s", Frames: []int{24, 51}}},
		Preserve: []string{"character"},
		Metrics: domain.JudgeMetrics{
			FaceIdentityStatus: domain.FaceMeasured,
			FaceIdentity:       []domain.FaceIdentityMetric{{CharacterID: "c", SimilarityMean: 0.71, SimilarityMin: 0.42, SampledFrames: 48, UndetectedRatio: 0.08, Evaluated: true}},
		},
		RepairStrategy: &domain.RepairStrategy{Action: domain.RepairPartialRegeneration, Instructions: []string{"reduce_camera_speed_40_percent"}},
	})
	require.NoError(t, err)

	latest, err := r.Latest(ctx, take)
	require.NoError(t, err)
	assert.Equal(t, second.ID, latest.ID)
	assert.Equal(t, domain.JudgeFail, latest.Result)
	assert.Equal(t, second.Issues, latest.Issues)
	assert.Equal(t, []string{"character"}, latest.Preserve)
	assert.Equal(t, second.Metrics, latest.Metrics)
	require.NotNil(t, latest.RepairStrategy)
	assert.Equal(t, domain.RepairPartialRegeneration, latest.RepairStrategy.Action)

	history, err := r.History(ctx, take)
	require.NoError(t, err)
	require.Len(t, history, 2, "the first run was not overwritten")
	assert.Equal(t, []string{second.ID, first.ID}, []string{history[0].ID, history[1].ID}, "newest first")
	assert.Nil(t, history[1].RepairStrategy, "a passing Take has no strategy")
	assert.Equal(t, []domain.JudgeIssue{}, history[1].Issues)
	assert.Equal(t, []string{}, history[1].Preserve)
}

func TestJudgeRepository_GetTakeReturnsTheSpecTheTakeWasMadeFrom(t *testing.T) {
	db := openTestDB(t)
	project, shot, take := seedTake(t, db)
	r := repository.NewJudgeRepository(db)

	ref, err := r.GetTake(context.Background(), take)
	require.NoError(t, err)
	assert.Equal(t, project, ref.ProjectID)
	assert.Equal(t, shot, ref.Take.ShotID)
	assert.Equal(t, "p1/a.mp4", ref.Take.FilePath)
	assert.JSONEq(t, `{"shot_objective":"run"}`, string(ref.SpecSnapshot))

	_, err = r.GetTake(context.Background(), "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestJudgeRepository_LocksListsOnlyLockedParametersOfTheProject(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l1', 'p1', 'character', 'c1', 'identity', 'lock')`,
			`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l2', 'p1', 'character', 'c1', 'wardrobe', 'manual')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	locks, err := repository.NewJudgeRepository(db).Locks(context.Background(), "p1")
	require.NoError(t, err)
	assert.Equal(t, []domain.LockKey{{TargetType: "character", TargetID: "c1", Parameter: "identity"}}, locks)
}

func TestJudgeRepository_ShotIsJudgingOnlyWhileNoOtherWorkHasIt(t *testing.T) {
	db := openTestDB(t)
	_, shot, take := seedTake(t, db)
	r := repository.NewJudgeRepository(db)
	ctx := context.Background()
	state := func() string {
		var s string
		require.NoError(t, db.Reader().QueryRow(`SELECT state FROM shot WHERE id = ?`, shot).Scan(&s))
		return s
	}
	insertJob := func(id, status string) {
		require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO job (id, kind, payload, status) VALUES (?, 'judge', ?, ?)`, id, `{"take_id":"`+take+`","shot_id":"`+shot+`"}`, status)
			return err
		}))
	}

	pending, err := r.JudgePending(ctx, take)
	require.NoError(t, err)
	assert.False(t, pending)

	insertJob("j1", "running")
	insertJob("j2", "queued")
	pending, err = r.JudgePending(ctx, take)
	require.NoError(t, err)
	assert.True(t, pending)

	require.NoError(t, r.BeginJudging(ctx, shot))
	assert.Equal(t, "JUDGING", state())

	require.NoError(t, r.EndJudging(ctx, shot, "j1"))
	assert.Equal(t, "JUDGING", state(), "another judge job (j2) of the Shot is still waiting")
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE job SET status = 'succeeded' WHERE id = 'j2'`)
		return err
	}))
	require.NoError(t, r.EndJudging(ctx, shot, "j1"))
	assert.Equal(t, "PLANNING", state())

	// A Shot that is being generated keeps its state.
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE shot SET state = 'GENERATING' WHERE id = ?`, shot)
		return err
	}))
	require.NoError(t, r.BeginJudging(ctx, shot))
	assert.Equal(t, "GENERATING", state())
}
