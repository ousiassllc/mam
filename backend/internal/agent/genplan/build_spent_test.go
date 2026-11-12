package genplan_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// The plan carries what each Shot to be made has already cost (the ledger, in one read for all of them), 0 for a Shot with nothing on it,
// and none for a Shot that is not to be made.
func TestBuilder_ShotSpentIsWhatTheLedgerHoldsForEachPlannedShot(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'a', 4, 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh2', 'sc1', 'b', 4, 1)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index, state) VALUES ('sh3', 'sc1', 'c', 4, 2, 'GENERATING')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	costs := repository.NewCostRepository(db)
	for _, shotID := range []string{"sh1", "sh3"} {
		require.NoError(t, costs.AddCost(ctx, domain.CostEntry{ProjectID: "p1", ShotID: shotID, Provider: domain.ModelKling3, EstimatedCost: 1.5}))
	}
	b := genplan.Builder{
		Plans: repository.NewPlanRepository(db), Gens: repository.NewGenerationRepository(db), Policies: repository.NewPolicyRepository(db),
		Config:     modeladapter.DefaultConfig,
		Candidates: func(context.Context, domain.Shot) (bool, error) { return false, nil },
	}

	plan, err := b.Build(ctx, "p1", nil)

	require.NoError(t, err)
	require.Len(t, plan.Lines, 2)
	assert.Equal(t, map[string]float64{"sh1": 1.5, "sh2": 0}, plan.ShotSpent)
	assert.InDelta(t, 3, plan.ProjectSpent, 1e-9)
}
