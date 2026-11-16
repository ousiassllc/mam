// Package assetmigrationtest_test tests the migration that lets a Project hold an audio file (asset.type = 'audio', Issue #1153). It is a package of its own because
// the directory of the repository is full (linterly).
package assetmigrationtest_test

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

// beforeAssetAudio is the migration before 20261116100000_asset_type_audio.sql.
const beforeAssetAudio = 20261115100000

type rig struct {
	t        *testing.T
	ctx      context.Context
	db       *sql.DB
	provider *goose.Provider
}

func newRig(t *testing.T) *rig {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1) // the writer pool of mam has one connection: the PRAGMAs of the migration hold for all of it
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, os.DirFS("../migrations"), goose.WithAllowOutofOrder(true))
	require.NoError(t, err)
	return &rig{t: t, ctx: context.Background(), db: db, provider: provider}
}

func (r *rig) exec(q string, args ...any) error {
	_, err := r.db.ExecContext(r.ctx, q, args...)
	return err
}

func (r *rig) must(queries ...string) {
	r.t.Helper()
	for _, q := range queries {
		require.NoError(r.t, r.exec(q), q)
	}
}

func (r *rig) count(q string) (n int) {
	r.t.Helper()
	require.NoError(r.t, r.db.QueryRowContext(r.ctx, q).Scan(&n), q)
	return n
}

func (r *rig) text(q string) (s sql.NullString) {
	r.t.Helper()
	require.NoError(r.t, r.db.QueryRowContext(r.ctx, q).Scan(&s), q)
	return s
}

// violations is what PRAGMA foreign_key_check reports: the rows whose foreign key finds no row.
func (r *rig) violations() int {
	r.t.Helper()
	rows, err := r.db.QueryContext(r.ctx, `PRAGMA foreign_key_check`)
	require.NoError(r.t, err)
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		n++
	}
	require.NoError(r.t, rows.Err())
	return n
}

// seed fills every table that points at asset, in the state before the migration: two Projects, an image, a reference, a video and a Library entry on them, a
// Character / Product / Location / Series that use them as reference images, and a chat message that carries one as an attachment.
func (r *rig) seed() {
	r.t.Helper()
	r.must(
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p', 'auto', 'key_check', 'product_cm', 'balanced'), ('p2', 'auto', 'key_check', 'product_cm', 'balanced')`,
		`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes, created_at) VALUES
			('a-img', 'p', 'image', 'p/a-img.png', 'image/png', 11, '2026-11-01T00:00:00.000Z'),
			('a-ref', 'p', 'reference', 'p/a-ref.png', 'image/png', 22, '2026-11-02T00:00:00.000Z'),
			('a-vid', 'p', 'video', 'p/a-vid.mp4', 'video/mp4', 33, '2026-11-03T00:00:00.000Z'),
			('b-img', 'p2', 'image', 'p2/b-img.png', NULL, NULL, '2026-11-04T00:00:00.000Z')`,
		`INSERT INTO character (id, project_id, name, face_reference_asset_id, body_reference_asset_id) VALUES ('c', 'p', 'Mio', 'a-img', 'a-ref')`,
		`INSERT INTO product (id, project_id, name, geometry_reference_asset_id, label_reference_asset_id) VALUES ('pr', 'p', 'Serum', 'a-img', 'a-ref')`,
		`INSERT INTO location (id, project_id, name, layout_reference_asset_id) VALUES ('l', 'p', 'Cafe', 'a-img')`,
		`INSERT INTO series (id, name, style_reference_asset_id) VALUES ('s', 'Show', 'a-img')`,
		`INSERT INTO reference_item (id, project_id, asset_id, kind, name, source) VALUES ('ri', 'p', 'a-ref', 'character', 'Mio', 'uploaded')`,
		`INSERT INTO chat_message (id, project_id, role, kind, content, attachment_asset_ids) VALUES ('m', 'p', 'user', 'text', 'look', '["a-img","a-vid"]')`,
	)
}

// The rebuilt table keeps every column of every row, its index and its rules; the tables that point at it keep their rows and their foreign keys, which find the new
// table under the old name; and the new type is accepted, checked by its exact name.
func TestMigration_AssetTypeAudioKeepsTheRowsTheIndexAndWhatPointsAtThem(t *testing.T) {
	r := newRig(t)
	_, err := r.provider.UpTo(r.ctx, beforeAssetAudio)
	require.NoError(t, err)
	r.seed()
	assert.Error(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path) VALUES ('x', 'p', 'audio', 'p/x.wav')`), "the table before knows no audio type")

	_, err = r.provider.Up(r.ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, r.count(`SELECT COUNT(*) FROM asset`), "every row is copied")
	var typ, path string
	var mime sql.NullString
	var size sql.NullInt64
	var created string
	require.NoError(t, r.db.QueryRowContext(r.ctx, `SELECT type, file_path, mime_type, size_bytes, created_at FROM asset WHERE id = 'a-ref'`).Scan(&typ, &path, &mime, &size, &created))
	assert.Equal(t, []any{"reference", "p/a-ref.png", "image/png", int64(22), "2026-11-02T00:00:00.000Z"}, []any{typ, path, mime.String, size.Int64, created})
	require.NoError(t, r.db.QueryRowContext(r.ctx, `SELECT mime_type, size_bytes FROM asset WHERE id = 'b-img'`).Scan(&mime, &size))
	assert.False(t, mime.Valid || size.Valid, "a NULL stays NULL")
	assert.Equal(t, 1, r.count(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_asset_project' AND tbl_name = 'asset'`))
	assert.Equal(t, 0, r.count(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('asset_new', 'asset_old')`), "no table of the swap is left behind")

	// what points at asset still does, and still finds its rows
	assert.Equal(t, 0, r.violations(), "foreign_key_check")
	assert.Equal(t, 1, r.count(`PRAGMA foreign_keys`), "foreign keys are on again")
	for _, table := range []string{"character", "product", "location", "series", "reference_item"} {
		assert.Positive(t, r.count(`SELECT COUNT(*) FROM pragma_foreign_key_list('`+table+`') WHERE "table" = 'asset'`), table+" still points at asset")
	}
	assert.Equal(t, 1, r.count(`SELECT COUNT(*) FROM character WHERE face_reference_asset_id = 'a-img' AND body_reference_asset_id = 'a-ref'`))
	assert.Equal(t, 1, r.count(`SELECT COUNT(*) FROM series WHERE style_reference_asset_id = 'a-img'`))
	assert.Equal(t, 1, r.count(`SELECT COUNT(*) FROM reference_item WHERE asset_id = 'a-ref'`))
	assert.Equal(t, `["a-img","a-vid"]`, r.text(`SELECT attachment_asset_ids FROM chat_message WHERE id = 'm'`).String)

	// the new type, by its exact name; the old rules stay
	require.NoError(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('a-wav', 'p', 'audio', 'p/a-wav.wav', 'audio/wav', 44)`))
	assert.Error(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path) VALUES ('bad', 'p', 'Audio', 'x')`))
	assert.Error(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path) VALUES ('bad', 'p', 'sound', 'x')`))
	assert.Error(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path) VALUES ('bad', 'nope', 'image', 'x')`), "the Project must exist")
	assert.Error(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path) VALUES ('a-img', 'p', 'image', 'x')`), "the id is the key")
	assert.Error(t, r.exec(`INSERT INTO asset (id, project_id, type) VALUES ('bad', 'p', 'image')`), "the file path is required")
	require.NoError(t, r.exec(`INSERT INTO character (id, project_id, name, face_reference_asset_id) VALUES ('c2', 'p', 'Ren', 'a-wav')`), "a foreign key finds the new audio row")
	assert.Error(t, r.exec(`INSERT INTO character (id, project_id, name, face_reference_asset_id) VALUES ('c3', 'p', 'Kai', 'missing')`), "and still refuses a missing one")
}

// The actions of the foreign keys work on the rebuilt table: removing an asset clears the reference columns that name it (ON DELETE SET NULL) and removes its
// Library entry (ON DELETE CASCADE); removing a Project removes its assets (ON DELETE CASCADE) and what hangs on them.
func TestMigration_AssetTypeAudioKeepsTheActionsOfTheForeignKeys(t *testing.T) {
	r := newRig(t)
	_, err := r.provider.UpTo(r.ctx, beforeAssetAudio)
	require.NoError(t, err)
	r.seed()
	_, err = r.provider.Up(r.ctx)
	require.NoError(t, err)

	require.NoError(t, r.exec(`DELETE FROM asset WHERE id = 'a-img'`))
	assert.False(t, r.text(`SELECT face_reference_asset_id FROM character WHERE id = 'c'`).Valid, "SET NULL: the face")
	assert.True(t, r.text(`SELECT body_reference_asset_id FROM character WHERE id = 'c'`).Valid, "the other asset's reference stays")
	assert.False(t, r.text(`SELECT geometry_reference_asset_id FROM product WHERE id = 'pr'`).Valid, "SET NULL: the product")
	assert.False(t, r.text(`SELECT layout_reference_asset_id FROM location WHERE id = 'l'`).Valid, "SET NULL: the location")
	assert.False(t, r.text(`SELECT style_reference_asset_id FROM series WHERE id = 's'`).Valid, "SET NULL: the series")
	assert.Equal(t, 1, r.count(`SELECT COUNT(*) FROM character`), "the Character itself stays")

	require.NoError(t, r.exec(`DELETE FROM asset WHERE id = 'a-ref'`))
	assert.Zero(t, r.count(`SELECT COUNT(*) FROM reference_item`), "CASCADE: the Library entry goes with its asset")
	assert.False(t, r.text(`SELECT body_reference_asset_id FROM character WHERE id = 'c'`).Valid)

	require.NoError(t, r.exec(`DELETE FROM project WHERE id = 'p'`))
	assert.Equal(t, 1, r.count(`SELECT COUNT(*) FROM asset`), "CASCADE: the assets of the Project go with it; the other Project's stays")
	assert.Zero(t, r.count(`SELECT COUNT(*) FROM chat_message`))
	assert.Equal(t, 0, r.violations())
}

// Going back drops the audio assets (the table before has no type for a sound) with the actions of the foreign keys, so that nothing is left pointing at them, and
// keeps every other row, the index and the rules; going up again works.
func TestMigration_AssetTypeAudioGoesBackWithoutLeavingADanglingReference(t *testing.T) {
	r := newRig(t)
	_, err := r.provider.UpTo(r.ctx, beforeAssetAudio)
	require.NoError(t, err)
	r.seed()
	_, err = r.provider.Up(r.ctx)
	require.NoError(t, err)
	r.must(
		`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('a-wav', 'p', 'audio', 'p/a-wav.wav', 'audio/wav', 44)`,
		`UPDATE character SET face_reference_asset_id = 'a-wav' WHERE id = 'c'`,
	)

	_, err = r.provider.DownTo(r.ctx, beforeAssetAudio)
	require.NoError(t, err)
	assert.Equal(t, 4, r.count(`SELECT COUNT(*) FROM asset`), "the sound is dropped, every other row stays")
	assert.Zero(t, r.count(`SELECT COUNT(*) FROM asset WHERE type = 'audio'`))
	assert.False(t, r.text(`SELECT face_reference_asset_id FROM character WHERE id = 'c'`).Valid, "what pointed at the sound is cleared")
	assert.Equal(t, "a-ref", r.text(`SELECT body_reference_asset_id FROM character WHERE id = 'c'`).String, "the other references stay")
	assert.Equal(t, 0, r.violations())
	assert.Equal(t, 1, r.count(`PRAGMA foreign_keys`))
	assert.Error(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path) VALUES ('x', 'p', 'audio', 'x')`))
	assert.Equal(t, 1, r.count(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_asset_project'`))
	require.NoError(t, r.exec(`DELETE FROM asset WHERE id = 'a-img'`))
	assert.False(t, r.text(`SELECT style_reference_asset_id FROM series WHERE id = 's'`).Valid, "the actions work after going back")

	_, err = r.provider.Up(r.ctx)
	require.NoError(t, err)
	require.NoError(t, r.exec(`INSERT INTO asset (id, project_id, type, file_path) VALUES ('again', 'p', 'audio', 'x')`))
}
