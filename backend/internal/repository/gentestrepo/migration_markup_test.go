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

// beforeInputMarkup is the migration of the pose role, the one the markup migration's table is built on.
const beforeInputMarkup = 20261103200000

// #1096: a markup image of GPT Image 2.6 is a role of gentest_input. The rebuilt table keeps what it had (every role, the rows, the strength, the index, the cascade).
func TestMigration_GentestInputRoleMarkup(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	exec := func(q string, args ...any) error { _, err := db.ExecContext(ctx, q, args...); return err }
	insert := func(id, role string) error {
		return exec(`INSERT INTO gentest_input (id, batch_id, role, file_path, mime_type, strength) VALUES (?, 'b', ?, 'x', 'image/png', 0.6)`, id, role)
	}
	count := func() (n int) {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gentest_input`).Scan(&n))
		return n
	}

	_, err = provider.UpTo(ctx, beforeInputMarkup)
	require.NoError(t, err)
	require.NoError(t, exec(`INSERT INTO gentest_batch (id, media) VALUES ('b', 'image')`))
	for _, role := range []string{"reference", "face", "style", "edit", "mask", "pose"} {
		require.NoError(t, insert("old-"+role, role))
	}
	assert.Error(t, insert("markup0", "markup"), "the table before knows no markup role")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 6, count(), "the rows of the old table are copied")
	require.NoError(t, insert("markup1", "markup"))
	for _, role := range []string{"reference", "first_frame", "last_frame", "face", "style", "edit", "mask", "pose"} {
		assert.NoError(t, insert("new-"+role, role), "the role %s is still allowed", role)
	}
	assert.Error(t, insert("bad", "middle"), "the role is still checked")
	var strength float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT strength FROM gentest_input WHERE id = 'markup1'`).Scan(&strength))
	assert.InDelta(t, 0.6, strength, 1e-9)
	var idx int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_gentest_input_batch'`).Scan(&idx))
	assert.Equal(t, 1, idx)

	_, err = provider.DownTo(ctx, beforeInputMarkup)
	require.NoError(t, err)
	var role string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT role FROM gentest_input WHERE id = 'markup1'`).Scan(&role))
	assert.Equal(t, "reference", role, "a markup input goes back to a reference")
	assert.Error(t, insert("markup2", "markup"))
	assert.NoError(t, insert("pose-again", "pose"), "the roles before are kept")

	require.NoError(t, exec(`DELETE FROM gentest_batch WHERE id = 'b'`))
	assert.Zero(t, count(), "ON DELETE CASCADE survives the rebuild")
	_, err = provider.Up(ctx)
	require.NoError(t, err)
}
