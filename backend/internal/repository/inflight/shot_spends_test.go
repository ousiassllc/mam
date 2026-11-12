package inflight_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ShotSpends is the ShotSpent of Budget for many Shots at once: the ledger (the actual cost when the
// vendor reported one, else the estimate) and the holds of generations in flight; a Shot with nothing on
// it is 0, and an entry of no Shot counts for none.
func TestShotSpends_MatchesBudgetOfEachShot(t *testing.T) {
	ctx := context.Background()
	db, projectID, shotID, pvID := seed(t)
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh2', 'sc1', 'b', 4, 1)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh3', 'sc1', 'c', 4, 2)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh4', 'sc1', 'd', 4, 3)`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	repo := repository.NewGenerationRepository(db)
	actual := 0.75
	_, err := repo.CreateTake(ctx, newTake(projectID, shotID, pvID, 1, 0.5))
	require.NoError(t, err)
	costs := repository.NewCostRepository(db)
	require.NoError(t, costs.AddCost(ctx, domain.CostEntry{ProjectID: projectID, ShotID: shotID, Provider: domain.ModelKling3, EstimatedCost: 1, ActualCost: &actual}))
	require.NoError(t, costs.AddCost(ctx, domain.CostEntry{ProjectID: projectID, ShotID: "sh2", Provider: domain.ModelKling3, EstimatedCost: 2}))
	require.NoError(t, costs.AddCost(ctx, domain.CostEntry{ProjectID: projectID, Provider: domain.ModelKling3, EstimatedCost: 4})) // no Shot
	release, ok, err := repo.Reserve(ctx, projectID, "sh3", 0.25)
	require.NoError(t, err)
	require.True(t, ok)
	defer release()

	ids := []string{shotID, "sh2", "sh3", "sh4"}
	got, err := repo.ShotSpends(ctx, projectID, ids)

	require.NoError(t, err)
	require.Len(t, got, 4)
	for _, id := range ids {
		b, err := repo.Budget(ctx, projectID, id)
		require.NoError(t, err)
		assert.InDelta(t, b.ShotSpent, got[id], 1e-9, id)
	}
	assert.InDelta(t, 1.25, got[shotID], 1e-9) // the take's estimate 0.5 + the entry's actual cost 0.75
	assert.InDelta(t, 2, got["sh2"], 1e-9)
	assert.InDelta(t, 0.25, got["sh3"], 1e-9, "a hold alone")
	assert.Zero(t, got["sh4"], "nothing on it")
}
