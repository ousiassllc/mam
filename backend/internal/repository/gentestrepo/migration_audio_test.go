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

// beforeInputAudio is the migration of the markup role, the one the audio migration's table is built on.
const beforeInputAudio = 20261110100400

// #1153: the sound of a speech-to-video model is a role of gentest_input. The rebuilt table keeps what it had (every role, the rows, the strength, the index, the
// cascade), and going back drops the sound (no role of the table before can hold a file no image reader opens).
func TestMigration_GentestInputRoleAudio(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	exec := func(q string, args ...any) error { _, err := db.ExecContext(ctx, q, args...); return err }
	insert := func(id, role string) error {
		return exec(`INSERT INTO gentest_input (id, batch_id, role, file_path, mime_type, size_bytes, position, strength) VALUES (?, 'b', ?, 'x', 'audio/wav', 44, 3, 0.6)`, id, role)
	}
	count := func() (n int) {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gentest_input`).Scan(&n))
		return n
	}

	_, err = provider.UpTo(ctx, beforeInputAudio)
	require.NoError(t, err)
	require.NoError(t, exec(`INSERT INTO gentest_batch (id, media) VALUES ('b', 'video')`))
	for _, role := range []string{"reference", "first_frame", "last_frame", "face", "style", "edit", "mask", "pose", "markup"} {
		require.NoError(t, insert("old-"+role, role))
	}
	assert.Error(t, insert("audio0", "audio"), "the table before knows no audio role")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 9, count(), "the rows of the old table are copied")
	require.NoError(t, insert("audio1", "audio"))
	assert.Error(t, insert("bad", "bogus"), "the role is still checked")
	assert.Error(t, insert("bad2", "Audio"), "and by its exact name")
	var size, position int
	var strength float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT size_bytes, position, strength FROM gentest_input WHERE id = 'old-markup'`).Scan(&size, &position, &strength))
	assert.Equal(t, 44, size)
	assert.Equal(t, 3, position)
	assert.InDelta(t, 0.6, strength, 1e-9)
	var idx int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_gentest_input_batch'`).Scan(&idx))
	assert.Equal(t, 1, idx)

	_, err = provider.DownTo(ctx, beforeInputAudio)
	require.NoError(t, err)
	assert.Equal(t, 9, count(), "the sound is dropped, every other row stays")
	assert.Error(t, insert("audio2", "audio"))
	assert.NoError(t, insert("markup-again", "markup"), "the roles before are kept")

	require.NoError(t, exec(`DELETE FROM gentest_batch WHERE id = 'b'`))
	assert.Zero(t, count(), "ON DELETE CASCADE survives the rebuild")
	_, err = provider.Up(ctx)
	require.NoError(t, err)
}
