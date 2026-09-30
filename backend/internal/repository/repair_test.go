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

func TestRepairRepository_TheNewTakeIsTracedAndTheRepairedTakeStaysAsItIs(t *testing.T) {
	db := openTestDB(t)
	_, shot, take := seedTake(t, db)
	ctx := context.Background()
	judged, err := repository.NewJudgeRepository(db).Save(ctx, domain.JudgeResult{
		TakeID: take, Result: domain.JudgeFail,
		Scores: map[domain.EvaluationAxis]float64{domain.AxisProductAccuracy: 61.5},
	})
	require.NoError(t, err)

	r := repository.NewRepairRepository(db)
	_, err = r.ProducedBy(ctx, take)
	require.ErrorIs(t, err, repository.ErrNotFound, "a first generation was made by no repair")

	rec, err := r.CreateAction(ctx, judged.ID, domain.RepairPrompt, domain.RepairInstructions{
		Instructions: []string{"reduce_camera_speed_40_percent"}, Reason: "camera", DecidedBy: domain.DecidedByLLM, SourceTakeID: take,
	})
	require.NoError(t, err)
	assert.Empty(t, rec.ResultingTakeID, "no Take yet")

	history, err := r.ShotActions(ctx, shot)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, 0, domain.RetriesDone(history), "a repair without a Take is no retry")

	// The new Take is a new row of its own.
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO take (id, prompt_version_id, model_name, file_path) VALUES ('t2', 'pv1', 'kling-3.0', 'p1/b.mp4')`)
		return err
	}))
	require.NoError(t, r.SetResultingTake(ctx, rec.ID, "t2"))

	made, err := r.ProducedBy(ctx, "t2")
	require.NoError(t, err)
	assert.Equal(t, rec.ID, made.ID)
	assert.Equal(t, domain.RepairPrompt, made.Action)
	assert.Equal(t, judged.ID, made.JudgeResultID)
	assert.Equal(t, take, made.Instructions.SourceTakeID)
	assert.Equal(t, []string{"reduce_camera_speed_40_percent"}, made.Instructions.Instructions)
	assert.Equal(t, domain.DecidedByLLM, made.Instructions.DecidedBy)

	history, err = r.ShotActions(ctx, shot)
	require.NoError(t, err)
	assert.Equal(t, 1, domain.RetriesDone(history))

	var status string
	require.NoError(t, db.Reader().QueryRow(`SELECT status FROM take WHERE id = ?`, take).Scan(&status))
	assert.Equal(t, string(domain.TakeCandidate), status, "the repaired Take is not touched")

	got, err := repository.NewJudgeRepository(db).Get(ctx, judged.ID)
	require.NoError(t, err)
	assert.InDelta(t, 61.5, got.Scores[domain.AxisProductAccuracy], 1e-9, "scores round-trip")
	_, err = repository.NewJudgeRepository(db).Get(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestRepairRepository_ShadowReportSummarisesAgreementConfidenceLatencyAndCost(t *testing.T) {
	db := openTestDB(t)
	_, _, take := seedTake(t, db)
	ctx := context.Background()
	judged, err := repository.NewJudgeRepository(db).Save(ctx, domain.JudgeResult{TakeID: take, Result: domain.JudgeFail})
	require.NoError(t, err)
	r := repository.NewRepairRepository(db)
	rec, err := r.CreateAction(ctx, judged.ID, domain.RepairFullRetake, domain.RepairInstructions{SourceTakeID: take, DecidedBy: domain.DecidedByLLM})
	require.NoError(t, err)

	empty, err := r.ShadowReport(ctx)
	require.NoError(t, err)
	assert.Zero(t, empty.Runs)
	assert.Zero(t, empty.AgreementRate)

	conf := func(v float64) *float64 { return &v }
	for _, s := range []domain.RepairShadow{
		{Confidence: conf(0.95), Agreed: true, LatencyMillis: 100, CostUSD: 0.001},
		{Confidence: conf(1.0), Agreed: true, LatencyMillis: 200, CostUSD: 0.001},
		{Confidence: conf(0.15), Agreed: false, LatencyMillis: 300, CostUSD: 0.002},
		{Error: "jev: status 500", LatencyMillis: 400},
	} {
		s.RepairActionID, s.Model, s.DecidedAction = rec.ID, "jev-1.13.0", domain.RepairFullRetake
		require.NoError(t, r.SaveShadow(ctx, s))
	}

	rep, err := r.ShadowReport(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, rep.Runs)
	assert.Equal(t, 1, rep.Errors)
	assert.Equal(t, 2, rep.Agreed)
	assert.InDelta(t, 2.0/3.0, rep.AgreementRate, 1e-9, "failed runs are left out of the rate")
	assert.InDelta(t, (0.95+1.0+0.15)/3, rep.MeanConfidence, 1e-9)
	assert.InDelta(t, 250, rep.MeanLatencyMillis, 1e-9)
	assert.InDelta(t, 0.004, rep.TotalCostUSD, 1e-9)
	assert.Equal(t, 1, rep.Confidence[1])
	assert.Equal(t, 2, rep.Confidence[domain.ConfidenceBuckets-1], "1.0 belongs to the last bucket")
	assert.Equal(t, 2, rep.ConfidenceAgreed[domain.ConfidenceBuckets-1])
	assert.Zero(t, rep.ConfidenceAgreed[1])
}

func TestGenerationRepository_ARepairAndAGenerationExcludeEachOther(t *testing.T) {
	db := openTestDB(t)
	_, shot, _ := seedTake(t, db)
	ctx := context.Background()
	g := repository.NewGenerationRepository(db)

	ok, err := g.StartRepair(ctx, shot)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = g.StartRepair(ctx, shot)
	require.NoError(t, err)
	assert.False(t, ok, "no second repair while one runs")
	ok, err = g.StartShot(ctx, shot)
	require.NoError(t, err)
	assert.False(t, ok, "no generation while the Shot is repaired")

	n, err := g.ResetStaleShots(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "an interrupted repair is released at start-up")
}
