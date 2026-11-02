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

// beforeInputPose is the migration of the strength, the last one the pose migration's table is built on; the inpaint mask migration
// (20261102120000_gentest_input_mask.sql, #974), when it is on the branch, is between the two.
const beforeInputPose = 20261101140000

// hasMaskMigration says whether the branch has the inpaint mask migration. Then the roles of the table after the pose migration
// must include "mask" as well as "pose": the pose migration rebuilds the table, and would drop the mask role if its list did not have it.
func hasMaskMigration(t *testing.T) bool {
	t.Helper()
	found, err := filepath.Glob("../migrations/*_gentest_input_mask.sql")
	require.NoError(t, err)
	return len(found) > 0
}

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
	if hasMaskMigration(t) {
		assert.NoError(t, insert("mask1", "mask"), "the inpaint mask role must stay allowed: add 'mask' to both role lists of the pose migration")
	}
	var strength float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT strength FROM gentest_input WHERE id = 'pose1'`).Scan(&strength))
	assert.InDelta(t, 0.6, strength, 1e-9)
	wantRows := 2
	if hasMaskMigration(t) {
		wantRows = 3
	}
	assert.Equal(t, wantRows, count(), "the rows of the old table are copied")
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
