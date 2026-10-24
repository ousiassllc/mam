// Package modelmigration holds the test of the migration that adds the job kinds of the model manager. It has its
// own directory because the repository directory is over the line limit.
package modelmigration_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/ousiassllc/mam/backend/internal/repository" // registers the sqlite driver
)

// beforeModelJobs is the migration before 20261025090000_job_kind_model.sql.
const beforeModelJobs = 20261024120000

// TestMigration_JobKindModelRebuildsTheJobTableBothWays: the rows survive the rebuild, the two kinds of the
// model manager are accepted afterwards, and going down drops those rows (the older CHECK refuses them) but
// keeps the others.
func TestMigration_JobKindModelRebuildsTheJobTableBothWays(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, beforeModelJobs)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO job (id, kind, payload, status, attempts) VALUES ('p', 'plan', '{"a":1}', 'running', 2)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO job (id, kind) VALUES ('x', 'model_install')`)
	require.Error(t, err, "the old CHECK refuses the new kinds")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var payload, status string
	var attempts int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT payload, status, attempts FROM job WHERE id = 'p'`).Scan(&payload, &status, &attempts))
	assert.Equal(t, `{"a":1}`, payload)
	assert.Equal(t, "running", status)
	assert.Equal(t, 2, attempts)
	for _, kind := range []string{"model_install", "gentest"} {
		_, err = db.ExecContext(ctx, `INSERT INTO job (id, kind) VALUES (?, ?)`, kind, kind)
		assert.NoError(t, err, kind)
	}

	_, err = provider.DownTo(ctx, beforeModelJobs)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job`).Scan(&n))
	assert.Equal(t, 1, n, "only the plan job is left")
	_, err = db.ExecContext(ctx, `INSERT INTO job (id, kind) VALUES ('y', 'gentest')`)
	assert.Error(t, err)
}
