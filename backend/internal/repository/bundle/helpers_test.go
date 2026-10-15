package bundle_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

func openTestDB(t *testing.T) *repository.DB {
	t.Helper()
	return testdb.Open(t)
}

func exec(t *testing.T, db *repository.DB, query string, args ...any) error {
	t.Helper()
	return db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	})
}
