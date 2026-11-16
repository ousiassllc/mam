// Package shotaudiotest is a database with two Projects, each with one Shot, and a shotaudiosvc.Service over it, for the tests of every entry point of the audio of a Shot (the
// service, the JSON API, the MCP tools). The audio files are real (tiny synthetic WAVs of package audiofiletest) under a temporary assets directory.
package shotaudiotest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
)

// Fixture has Project "p1" (Scene 1, Shot "s1", whose spec has a storyboard) and Project "p2" (Shot "other"), and no asset.
type Fixture struct {
	T      *testing.T
	DB     *repository.DB
	Assets string
	Svc    *shotaudiosvc.Service
	// Recorded are the hand edits the Service noted.
	Recorded []domain.Intervention
	// Limit is the longest sound the Service accepts (seconds); it starts at 60.
	Limit int
}

// New returns the Fixture.
func New(t *testing.T) *Fixture {
	t.Helper()
	db := testdb.Open(t)
	f := &Fixture{T: t, DB: db, Assets: t.TempDir(), Limit: 60}
	f.Svc = shotaudiosvc.New(shotaudiosvc.Deps{
		DB: db, Shots: repository.NewGenerationRepository(db), Assets: repository.NewAssetRepository(db), AssetsDir: f.Assets,
		Record:     func(_ context.Context, in domain.Intervention) { f.Recorded = append(f.Recorded, in) },
		MaxSeconds: func() int { return f.Limit },
	})
	for _, p := range []string{"p1", "p2"} {
		f.Exec(`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES (?, 'auto', 'key_check', 'product_cm', 'balanced')`, p)
		f.Exec(`INSERT INTO scene (id, project_id, order_index) VALUES (?, ?, 0)`, "sc-"+p, p)
	}
	f.Exec(`INSERT INTO shot (id, scene_id, order_index, shot_spec) VALUES ('s1', 'sc-p1', 0, '{"storyboard":{"description":"d"}}')`)
	f.Exec(`INSERT INTO shot (id, scene_id, order_index) VALUES ('other', 'sc-p2', 0)`)
	return f
}

// Exec runs a statement.
func (f *Fixture) Exec(q string, args ...any) {
	f.T.Helper()
	require.NoError(f.T, f.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// Asset registers an asset of the Project whose file holds data (nil data: no file is written).
func (f *Fixture) Asset(project, id string, typ domain.AssetType, data []byte) {
	f.T.Helper()
	path := id + ".bin"
	if data != nil {
		require.NoError(f.T, os.WriteFile(filepath.Join(f.Assets, path), data, 0o600))
	}
	f.Exec(`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes) VALUES (?, ?, ?, ?, 'audio/wav', ?)`, id, project, string(typ), path, len(data))
}

// Spec is the shot_spec of the Shot.
func (f *Fixture) Spec(shot string) []byte {
	f.T.Helper()
	var raw string
	require.NoError(f.T, f.DB.Reader().QueryRow(`SELECT shot_spec FROM shot WHERE id = ?`, shot).Scan(&raw))
	return []byte(raw)
}

// WAV is a WAV of the given number of seconds.
func WAV(seconds int) []byte { return audiofiletest.WAV(8000, 8000*seconds) }
