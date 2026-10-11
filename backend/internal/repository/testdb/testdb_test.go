package testdb_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

func countProjects(t *testing.T, db *repository.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.Reader().QueryRow(`SELECT count(*) FROM project`).Scan(&n))
	return n
}

func TestOpen_IsMigrated(t *testing.T) {
	db := testdb.Open(t)
	var n int
	require.NoError(t, db.Reader().QueryRow(`SELECT count(*) FROM goose_db_version`).Scan(&n))
	assert.Greater(t, n, 1)
	assert.Equal(t, 0, countProjects(t, db))
}

func TestOpen_CopiesDoNotShareData(t *testing.T) {
	first := testdb.Open(t)
	require.NoError(t, first.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`)
		return err
	}))
	require.Equal(t, 1, countProjects(t, first))

	assert.Equal(t, 0, countProjects(t, testdb.Open(t)))
}

func TestPath_IsAClosedFileWithoutWAL(t *testing.T) {
	path := testdb.Path(t)
	_, err := os.Stat(path)
	require.NoError(t, err)
	_, err = os.Stat(path + "-wal")
	assert.True(t, os.IsNotExist(err))

	db, err := repository.Open(context.Background(), path, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assert.Equal(t, 0, countProjects(t, db))
}
