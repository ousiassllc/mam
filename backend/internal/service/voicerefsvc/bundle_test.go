package voicerefsvc_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// app is a whole instance: its database and its assets directory.
type app struct {
	svc    *service.Services
	assets string
	db     interface {
		Write(context.Context, func(context.Context, *sql.Tx) error) error
	}
}

func newApp(t *testing.T) app {
	t.Helper()
	db := testdb.Open(t)
	dir := t.TempDir()
	return app{svc: service.New(service.Deps{DB: db, AssetsDir: dir}), assets: dir, db: db}
}

func (a app) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	require.NoError(t, a.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// move exports the Project from one instance and imports it into the other.
func move(t *testing.T, from, to app, projectID string) {
	t.Helper()
	ctx := context.Background()
	exp, err := from.svc.Bundle.Prepare(ctx, projectID)
	require.NoError(t, err)
	var zipped bytes.Buffer
	require.NoError(t, exp.Write(&zipped))
	_, err = to.svc.Bundle.Import(ctx, bytes.NewReader(zipped.Bytes()), int64(zipped.Len()))
	require.NoError(t, err)
}

func TestBundle_AReferenceVoiceSurvivesExportAndImportWhereverItsFileLived(t *testing.T) {
	ctx := context.Background()
	src := newApp(t)
	src.exec(t, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	src.exec(t, `INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ')`)
	recording := wav(40000)

	// A Project that is not in a Series: the file lives in its own directory.
	own, err := src.svc.VoiceRef.SetForProject(ctx, "p1", "c1", voiceInput(recording, "元の声"))
	require.NoError(t, err)

	// A Series made from it, a recording set on the canon (the Series' directory), and an Episode that copied it.
	series, err := src.svc.Series.Create(ctx, "ユイの日常", "p1")
	require.NoError(t, err)
	canon, err := src.svc.Series.View(ctx, series.ID)
	require.NoError(t, err)
	canonRef, err := src.svc.VoiceRef.SetForSeries(ctx, series.ID, canon.Canon.Characters[0].ID, voiceInput(recording, "正本の声"))
	require.NoError(t, err)
	ep2, err := src.svc.Series.CreateEpisode(ctx, series.ID, "", "")
	require.NoError(t, err)

	for name, c := range map[string]struct {
		projectID string
		wantText  string
		wasAt     string
	}{
		"its own file":                   {"p1", "元の声", own.FilePath},
		"a file of the Series directory": {ep2.ID, "正本の声", canonRef.FilePath},
	} {
		dst := newApp(t)
		move(t, src, dst, c.projectID)

		voice, err := dst.svc.VoiceRef.ForSpeaker(ctx, c.projectID, "", "ユイ")
		require.NoError(t, err, name)
		assert.Equal(t, c.wantText, voice.Reference.Text, name)
		assert.True(t, filepath.ToSlash(voice.Reference.FilePath)[:len(c.projectID)+1] == c.projectID+"/", "%s: the file is in the directory of the imported Project: %s", name, voice.Reference.FilePath)
		got, err := os.ReadFile(voice.Path)
		require.NoError(t, err, name)
		assert.Equal(t, recording, got, "%s: the bytes are the same", name)
		if c.projectID != "p1" {
			assert.NotEqual(t, c.wasAt, voice.Reference.FilePath, "%s: a shared file is moved into the Project", name)
		}
	}
}

func TestBundle_TwoCharactersSharingOneRecordingExportItOnce(t *testing.T) {
	ctx := context.Background()
	src := newApp(t)
	src.exec(t, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	src.exec(t, `INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ')`)
	_, err := src.svc.VoiceRef.SetForProject(ctx, "p1", "c1", voiceInput(wav(100), ""))
	require.NoError(t, err)
	series, err := src.svc.Series.Create(ctx, "S", "p1")
	require.NoError(t, err)
	ep2, err := src.svc.Series.CreateEpisode(ctx, series.ID, "", "")
	require.NoError(t, err)
	src.exec(t, `INSERT INTO character (id, project_id, name, voice_reference) SELECT 'c-copy', project_id, name, voice_reference FROM character WHERE project_id = ? AND branch_id IS NULL`, ep2.ID)

	dst := newApp(t)
	move(t, src, dst, ep2.ID)
	files, err := os.ReadDir(filepath.Join(dst.assets, ep2.ID))
	require.NoError(t, err)
	assert.Len(t, files, 1, "both rows name one file")
}
