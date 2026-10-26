package inflight_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

// seed makes a Project with one Shot and one Prompt Version.
func seed(t *testing.T) (db *repository.DB, projectID, shotID, promptVersionID string) {
	t.Helper()
	db = testdb.Open(t)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{}')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	return db, "p1", "sh1", "pv1"
}

func newTake(projectID, shotID, pvID string, n int, cost float64) repository.NewTake {
	return repository.NewTake{
		ProjectID: projectID, ShotID: shotID, PromptVersionID: pvID, Model: domain.ModelKling3, EstimatedCost: cost,
		Asset: domain.Asset{ID: fmt.Sprintf("a%d", n), ProjectID: projectID, Type: domain.AssetVideo, FilePath: fmt.Sprintf("p/%d.mp4", n), MimeType: "video/mp4"},
	}
}

// The cost of a Take moves from its hold to the ledger in one step for a Reserve: counted in both for
// a moment it refused a Candidate that fitted the Shot's budget (#840). A ceiling of 1.0 pays for two
// Takes of 0.5: while the first is stored, a second hold always fits.
func TestCreateReservedTake_AReserveWhileTheTakeIsStoredCountsTheCostOnce(t *testing.T) {
	ctx := context.Background()
	for n := 1; n <= 25; n++ {
		db, projectID, shotID, pvID := seed(t)
		repo := repository.NewGenerationRepository(db)
		require.NoError(t, repository.NewPolicyRepository(db).SetBudget(ctx, projectID, nil, new(1.0)))
		release, ok, err := repo.Reserve(ctx, projectID, shotID, 0.5)
		require.NoError(t, err)
		require.True(t, ok)

		stop, done := make(chan struct{}), make(chan struct{})
		var refused int
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				default:
				}
				second, ok, err := repo.Reserve(ctx, projectID, shotID, 0.5)
				if err != nil || !ok {
					refused++
					continue
				}
				second()
			}
		}()
		_, err = repo.CreateReservedTake(ctx, newTake(projectID, shotID, pvID, n, 0.5), release)
		close(stop)
		<-done
		require.NoError(t, err)
		require.Zero(t, refused, "a second Take that fits was refused while the first was stored")
	}
}

func TestCreateReservedTake_KeepsTheHoldWhenTheTakeIsNotStored(t *testing.T) {
	ctx := context.Background()
	db, projectID, shotID, _ := seed(t)
	repo := repository.NewGenerationRepository(db)
	release, ok, err := repo.Reserve(ctx, projectID, shotID, 0.3)
	require.NoError(t, err)
	require.True(t, ok)
	defer release()

	_, err = repo.CreateReservedTake(ctx, newTake(projectID, shotID, "missing", 1, 0.3), release)

	require.Error(t, err)
	b, err := repo.Budget(ctx, projectID, shotID)
	require.NoError(t, err)
	assert.InDelta(t, 0.3, b.ShotSpent, 1e-9, "the caller still holds the reservation")
}
