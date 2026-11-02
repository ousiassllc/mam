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

// beforeInputStrength is the migration before 20261101140000_gentest_input_role_strength.sql.
const beforeInputStrength = 20261101130000

func TestMigration_GentestInputRoleAndStrength(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	exec := func(q string, args ...any) error { _, err := db.ExecContext(ctx, q, args...); return err }
	insert := func(id, role string) error {
		return exec(`INSERT INTO gentest_input (id, batch_id, role, file_path, mime_type) VALUES (?, 'b', ?, 'x', 'image/png')`, id, role)
	}
	hasStrength := func() bool {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('gentest_input') WHERE name = 'strength'`).Scan(&n))
		return n == 1
	}

	_, err = provider.UpTo(ctx, beforeInputStrength)
	require.NoError(t, err)
	require.NoError(t, exec(`INSERT INTO gentest_batch (id, media) VALUES ('b', 'image')`))
	require.NoError(t, insert("old", "reference"))
	assert.Error(t, insert("face0", "face"), "the old table knows no face role")
	assert.False(t, hasStrength())

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.True(t, hasStrength())
	var strength sql.NullFloat64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT strength FROM gentest_input WHERE id = 'old'`).Scan(&strength))
	assert.False(t, strength.Valid, "a copied row has the default strength (NULL)")
	for _, role := range []string{"face", "style", "edit", "first_frame", "last_frame"} {
		assert.NoError(t, insert("n-"+role, role), role)
	}
	assert.Error(t, insert("bad", "middle"), "the role is still checked")
	require.NoError(t, exec(`UPDATE gentest_input SET strength = 0.4 WHERE id = 'n-face'`))
	assert.Error(t, exec(`UPDATE gentest_input SET strength = 1.2 WHERE id = 'n-face'`), "the strength is within 0..1")
	var idx int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_gentest_input_batch'`).Scan(&idx))
	assert.Equal(t, 1, idx)

	_, err = provider.DownTo(ctx, beforeInputStrength)
	require.NoError(t, err)
	assert.False(t, hasStrength())
	var role string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT role FROM gentest_input WHERE id = 'n-face'`).Scan(&role))
	assert.Equal(t, "reference", role, "a face reference goes back to the whole picture")
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gentest_input`).Scan(&n))
	assert.Equal(t, 6, n)
	require.NoError(t, exec(`DELETE FROM gentest_batch WHERE id = 'b'`))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gentest_input`).Scan(&n))
	assert.Zero(t, n, "ON DELETE CASCADE survives the rebuild")
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.True(t, hasStrength())
}
