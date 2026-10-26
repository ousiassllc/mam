package gentestrepo_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// beforeGentest is the migration before 20261025150000_gentest.sql.
const beforeGentest = 20261025120000

func TestMigration_GentestUpAndDown(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	tables := func() int {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE 'gentest_%'`).Scan(&n))
		return n
	}

	_, err = provider.UpTo(ctx, beforeGentest)
	require.NoError(t, err)
	assert.Zero(t, tables())
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, tables())

	exec := func(q string, args ...any) error { _, err := db.ExecContext(ctx, q, args...); return err }
	require.NoError(t, exec(`INSERT INTO gentest_batch (id, media) VALUES ('b', 'image')`))
	assert.Error(t, exec(`INSERT INTO gentest_batch (id, media) VALUES ('x', 'audio')`), "media is image or video")
	assert.Error(t, exec(`INSERT INTO gentest_result (id, batch_id, media, model, prompt, status) VALUES ('r0', 'b', 'image', 'm', 'p', 'done')`), "status is checked")
	assert.Error(t, exec(`INSERT INTO gentest_result (id, batch_id, media, model, prompt, negative_mode) VALUES ('r0', 'b', 'image', 'm', 'p', 'dropped')`), "negative_mode is checked")
	assert.Error(t, exec(`INSERT INTO gentest_result (id, batch_id, media, model, prompt, cost_basis) VALUES ('r0', 'b', 'image', 'm', 'p', 'cheap')`), "cost_basis is checked")
	assert.Error(t, exec(`INSERT INTO gentest_result (id, batch_id, media, model, prompt) VALUES ('r0', 'nope', 'image', 'm', 'p')`), "a result needs its batch")
	require.NoError(t, exec(`INSERT INTO gentest_result (id, batch_id, media, model, prompt) VALUES ('r1', 'b', 'image', 'm', 'p')`))
	require.NoError(t, exec(`INSERT INTO gentest_input (id, batch_id, role, file_path, mime_type) VALUES ('i1', 'b', 'reference', 'x', 'image/png')`))
	assert.Error(t, exec(`INSERT INTO gentest_input (id, batch_id, role, file_path, mime_type) VALUES ('i2', 'b', 'middle', 'x', 'image/png')`), "role is checked")
	require.NoError(t, exec(`INSERT INTO gentest_prompt (id, prompt) VALUES ('p1', 'a')`))
	assert.Error(t, exec(`INSERT INTO gentest_prompt (id, prompt) VALUES ('p2', 'a')`), "(prompt, negative_prompt) is unique")
	require.NoError(t, exec(`INSERT INTO gentest_import (id, result_id, project_id, asset_id) VALUES ('m1', 'r1', 'gone-project', 'gone-asset')`))

	require.NoError(t, exec(`DELETE FROM gentest_batch WHERE id = 'b'`))
	for _, table := range []string{"gentest_result", "gentest_input", "gentest_import"} {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n))
		assert.Zero(t, n, "deleting the batch deletes %s", table)
	}
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gentest_prompt`).Scan(&n))
	assert.Equal(t, 1, n, "the prompt history is independent of the results")

	_, err = provider.DownTo(ctx, beforeGentest)
	require.NoError(t, err)
	assert.Zero(t, tables())
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, tables(), "up again after down")
}
