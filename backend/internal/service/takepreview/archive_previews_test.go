//go:build unix

package takepreview_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// archiveFixture is a Shot whose Selected Take is "t1" and whose other Takes, "t2" and "t3", are
// unneeded. Each Take has a video, a proxy and a thumbnail in the preview directory.
type archiveFixture struct {
	svc      *service.Services
	assets   string
	previews string
}

func newArchiveFixture(t *testing.T) archiveFixture {
	t.Helper()
	f := archiveFixture{assets: filepath.Join(t.TempDir(), "assets")}
	f.previews = filepath.Join(f.assets, ".previews")
	require.NoError(t, os.MkdirAll(f.previews, 0o750))
	db := testdb.Open(t)
	stmts := []string{
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
		`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
		`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 2, 0)`,
		`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{}')`,
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		file := "p1/" + id + ".mp4"
		require.NoError(t, os.MkdirAll(filepath.Join(f.assets, "p1"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(f.assets, file), make([]byte, 100), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(f.previews, id+".mp4"), make([]byte, 1024), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(f.previews, id+".jpg"), make([]byte, 1024), 0o600))
		stmts = append(stmts,
			`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES ('a`+id+`', 'p1', 'video', '`+file+`', 'video/mp4', 100)`,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path, status) VALUES ('`+id+`', 'pv1', 'kling-3.0', '{}', '`+file+`', 'candidate')`)
	}
	stmts = append(stmts, `UPDATE take SET status = 'selected' WHERE id = 't1'`, `UPDATE shot SET selected_take_id = 't1' WHERE id = 'sh1'`)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range stmts {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	f.svc = service.New(service.Deps{DB: db, AssetsDir: f.assets})
	t.Cleanup(f.svc.Close)
	return f
}

func (f archiveFixture) previewsOf(id string) []string {
	return []string{filepath.Join(f.previews, id+".mp4"), filepath.Join(f.previews, id+".jpg")}
}

// The archive is how the user gets the disk back: the proxy and the thumbnail of an archived Take
// are deleted with its video, and the preview cache is part of what the Storage screen shows.
func TestArchiveTake_DeletesThePreviewsOfTheTake(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	view, err := f.svc.Policy.Storage(ctx, "p1")
	require.NoError(t, err)
	assert.EqualValues(t, 6*1024, view.PreviewBytes)

	view, err = f.svc.Policy.ArchiveTake(ctx, "p1", "t2")

	require.NoError(t, err)
	for _, p := range f.previewsOf("t2") {
		assert.NoFileExists(t, p)
	}
	for _, p := range append(f.previewsOf("t1"), f.previewsOf("t3")...) {
		assert.FileExists(t, p, "the previews of the other Takes stay")
	}
	assert.EqualValues(t, 4*1024, view.PreviewBytes)
}

func TestArchiveUnneeded_DeletesThePreviewsOfEveryArchivedTakeButNotOfTheSelectedOne(t *testing.T) {
	f := newArchiveFixture(t)

	view, err := f.svc.Policy.ArchiveUnneeded(context.Background(), "p1")

	require.NoError(t, err)
	for _, p := range append(f.previewsOf("t2"), f.previewsOf("t3")...) {
		assert.NoFileExists(t, p)
	}
	for _, p := range f.previewsOf("t1") {
		assert.FileExists(t, p)
	}
	assert.EqualValues(t, 2*1024, view.PreviewBytes)
	assert.Equal(t, domain.TakeSelected, view.Takes[0].Status)
}
