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

// beforeInputPose is the migration of the strength, the last one this migration's table is built on in every order the sweep's migrations
// land in (the inpaint mask migration of 20261102120000 is between the two when it is there).
const beforeInputPose = 20261101140000

// #986: a pose reference is a role of gentest_input. The rebuilt table keeps what it had (rows, strength, the index, the cascade).
func TestMigration_GentestInputRolePose(t *testing.T) {
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

	_, err = provider.UpTo(ctx, beforeInputPose)
	require.NoError(t, err)
	require.NoError(t, exec(`INSERT INTO gentest_batch (id, media) VALUES ('b', 'image')`))
	require.NoError(t, insert("whole", "reference"))
	assert.Error(t, insert("pose0", "pose"), "the table before knows no pose role")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	require.NoError(t, insert("pose1", "pose"))
	assert.Error(t, insert("bad", "middle"), "the role is still checked")
	assert.NoError(t, insert("mask1", "mask"), "the mask of the inpaint migration is allowed too")
	var strength float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT strength FROM gentest_input WHERE id = 'pose1'`).Scan(&strength))
	assert.InDelta(t, 0.6, strength, 1e-9)
	assert.Equal(t, 3, count(), "the rows of the old table are copied")
	var idx int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_gentest_input_batch'`).Scan(&idx))
	assert.Equal(t, 1, idx)

	_, err = provider.DownTo(ctx, beforeInputPose)
	require.NoError(t, err)
	var role string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT role FROM gentest_input WHERE id = 'pose1'`).Scan(&role))
	assert.Equal(t, "reference", role, "a pose reference goes back to the whole picture")
	assert.Error(t, insert("pose2", "pose"))

	require.NoError(t, exec(`DELETE FROM gentest_batch WHERE id = 'b'`))
	assert.Zero(t, count(), "ON DELETE CASCADE survives the rebuild")
	_, err = provider.Up(ctx)
	require.NoError(t, err)
}
