package service_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// bundleZip is a bundle of Project "proj1" with one Asset row naming proj1/named.bin and the given
// files (asset-relative path → content); a file is stored deflated, so it can be big on disk and
// small in the archive.
func bundleZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	return bundleZipWithTables(t, "", files)
}

// bundleZipWithTables is bundleZip with more tables: extraTables is JSON table objects, each
// preceded by a comma, added after the asset table.
func bundleZipWithTables(t *testing.T, extraTables string, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, content []byte) {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(content)
		require.NoError(t, err)
	}
	add("manifest.json", []byte(`{"format":"mam-project-bundle","version":1,"project_id":"proj1"}`))
	add("data.json", []byte(`{"project_id":"proj1","tables":[
		{"name":"project","rows":[{"id":"proj1","state":"DRAFT","mode":"auto","confirmation_level":"key_check","production_template":"product_cm","quality_policy":"balanced"}]},
		{"name":"asset","rows":[{"id":"a1","project_id":"proj1","type":"image","file_path":"proj1/named.bin"}]}`+extraTables+`]}`))
	for rel, content := range files {
		add("assets/"+rel, content)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func importBundle(f genFixture, data []byte) error {
	_, err := f.svc.Bundle.Import(context.Background(), bytes.NewReader(data), int64(len(data)))
	return err
}

func TestBundleImport_ExtractsOnlyTheFilesTheDataNames(t *testing.T) {
	f := newGenFixture(t, false)
	require.NoError(t, importBundle(f, bundleZip(t, map[string][]byte{
		"proj1/named.bin":  []byte("kept"),
		"proj1/orphan.bin": bytes.Repeat([]byte{0}, 1<<20),
		"proj1/deep/x/y.z": []byte("nothing refers to me"),
	})))

	kept, err := os.ReadFile(filepath.Join(f.assetsDir, "proj1", "named.bin"))
	require.NoError(t, err)
	assert.Equal(t, "kept", string(kept))
	var extracted []string
	require.NoError(t, filepath.WalkDir(filepath.Join(f.assetsDir, "proj1"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			extracted = append(extracted, filepath.Base(p))
		}
		return err
	}))
	assert.Equal(t, []string{"named.bin"}, extracted, "the unreferenced entries were not written")
}

func TestBundleImport_RefusesFilesThatDoNotFitTheFreeDiskAndWritesNothing(t *testing.T) {
	f := newGenFixtureWithDeps(t, false, func(string) (uint64, error) { return 1 << 20, nil }, nil) // 1 MiB free
	data := bundleZip(t, map[string][]byte{"proj1/named.bin": bytes.Repeat([]byte{0}, 4<<20)})      // 4 MiB, tiny once deflated
	require.Less(t, len(data), 1<<20)

	var ve *service.ValidationError
	require.ErrorAs(t, importBundle(f, data), &ve)
	assert.Contains(t, ve.Message, "空き容量")

	_, err := os.Stat(filepath.Join(f.assetsDir, "proj1"))
	assert.True(t, os.IsNotExist(err), "no file was written")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM project WHERE id = 'proj1'`))

	roomy := newGenFixtureWithDeps(t, false, func(string) (uint64, error) { return 8 << 20, nil }, nil)
	assert.NoError(t, importBundle(roomy, data), "the same bundle imports when the disk has room")
}

// An exported bundle carries the files only the Audio Timeline names (mix, clips, Lip Sync
// pictures), so a Project that was exported and imported again can still be exported to video.
func TestBundleExport_RoundTripKeepsTheAudioTimelineFiles(t *testing.T) {
	timeline := `{\"edit_timeline_id\":\"e1\",\"clips\":[{\"path\":\"proj1/audio/clip.wav\"}],` +
		`\"lip_sync\":[{\"clip_index\":1,\"take_id\":\"t1\",\"path\":\"proj1/audio/lip.png\"}],\"mix_path\":\"proj1/audio/mix.wav\"}`
	src := bundleZipWithTables(t, `,{"name":"audio_timeline_version","rows":[{"id":"at1","project_id":"proj1","version_number":1,"timeline":"`+timeline+`"}]}`,
		map[string][]byte{
			"proj1/named.bin":      []byte("named"),
			"proj1/audio/mix.wav":  []byte("mix"),
			"proj1/audio/clip.wav": []byte("clip"),
			"proj1/audio/lip.png":  []byte("lip"),
		})
	first := newGenFixture(t, false)
	require.NoError(t, importBundle(first, src))

	exp, err := first.svc.Bundle.Prepare(context.Background(), "proj1")
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, exp.Write(&out))

	second := newGenFixture(t, false)
	require.NoError(t, importBundle(second, out.Bytes()))
	for rel, want := range map[string]string{"named.bin": "named", "audio/mix.wav": "mix", "audio/clip.wav": "clip", "audio/lip.png": "lip"} {
		got, err := os.ReadFile(filepath.Join(second.assetsDir, "proj1", filepath.FromSlash(rel)))
		require.NoError(t, err, rel)
		assert.Equal(t, want, string(got), rel)
	}
}

// An Episode's copy of a canon image shares the file of the Series' first Project. The bundle of
// the Episode carries that file under the Episode's own directory, so it imports into another
// instance and the image is there.
func TestBundleExport_RoundTripRelocatesAFileSharedWithAnotherProject(t *testing.T) {
	src := bundleZip(t, map[string][]byte{"proj1/named.bin": []byte("named")})
	first := newGenFixture(t, false)
	require.NoError(t, importBundle(first, src))
	shared := filepath.Join(first.assetsDir, "series-first", "face.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(shared), 0o755))
	require.NoError(t, os.WriteFile(shared, []byte("face"), 0o600))
	require.NoError(t, first.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE asset SET file_path = 'series-first/face.png' WHERE id = 'a1'`)
		return err
	}))

	exp, err := first.svc.Bundle.Prepare(context.Background(), "proj1")
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, exp.Write(&out))

	second := newGenFixture(t, false)
	require.NoError(t, importBundle(second, out.Bytes()))
	filePath := second.text(t, `SELECT file_path FROM asset WHERE id = 'a1'`)
	assert.Equal(t, "proj1/a1.png", filePath)
	got, err := os.ReadFile(filepath.Join(second.assetsDir, filepath.FromSlash(filePath)))
	require.NoError(t, err)
	assert.Equal(t, "face", string(got))
}

func TestBundlePrepare_RefusesAFilePathItCannotCarry(t *testing.T) {
	first := newGenFixture(t, false)
	require.NoError(t, importBundle(first, bundleZip(t, map[string][]byte{"proj1/named.bin": []byte("named")})))
	require.NoError(t, first.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE asset SET file_path = '../../etc/passwd' WHERE id = 'a1'`)
		return err
	}))
	_, err := first.svc.Bundle.Prepare(context.Background(), "proj1")
	assert.Error(t, err)
}
