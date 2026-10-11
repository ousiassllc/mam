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

// An action is refused by the rule the API and the pages offer it by (domain.Take.Usable and
// Selectable): a rejected Take stays excluded, and nothing changes for a refused action.
func TestTakeRepository_SelectRefusesARejectedTakeAndTheSelectedOne(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 3)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()
	_, err := r.Select(ctx, "t1")
	require.NoError(t, err)
	_, err = r.Reject(ctx, "t2")
	require.NoError(t, err)

	_, rejected := r.Select(ctx, "t2")
	_, again := r.Select(ctx, "t1")

	assert.ErrorIs(t, rejected, repository.ErrConflict)
	assert.ErrorIs(t, again, repository.ErrConflict)
	assert.Equal(t, map[string]domain.TakeStatus{"t1": domain.TakeSelected, "t2": domain.TakeRejected, "t3": domain.TakeCandidate}, statuses(t, r))
	assert.Equal(t, "t1", selectedTake(t, db), "the excluded Take did not come back as the Shot's Take")
}

func TestTakeRepository_RejectRefusesARejectedTakeAndOneWhoseFileWasArchived(t *testing.T) {
	db := openTestDB(t)
	seedTake(t, db)
	addTakes(t, db, 3)
	r := repository.NewTakeRepository(db)
	ctx := context.Background()
	require.NoError(t, db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE take SET file_path = NULL WHERE id = 't3'`)
		return err
	}))
	_, err := r.Reject(ctx, "t2")
	require.NoError(t, err)

	_, again := r.Reject(ctx, "t2")
	_, archived := r.Reject(ctx, "t3")

	assert.ErrorIs(t, again, repository.ErrConflict)
	assert.ErrorIs(t, archived, repository.ErrConflict)
	assert.Equal(t, map[string]domain.TakeStatus{"t1": domain.TakeCandidate, "t2": domain.TakeRejected, "t3": domain.TakeCandidate}, statuses(t, r))
}
