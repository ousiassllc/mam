package bundle_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

func openTestDB(t *testing.T) *repository.DB {
	t.Helper()
	db, err := repository.Open(context.Background(), filepath.Join(t.TempDir(), "mam.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func exec(t *testing.T, db *repository.DB, query string, args ...any) error {
	t.Helper()
	return db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	})
}
