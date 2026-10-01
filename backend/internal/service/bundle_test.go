package service_test

import (
	"archive/zip"
	"bytes"
	"context"
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
		{"name":"asset","rows":[{"id":"a1","project_id":"proj1","type":"image","file_path":"proj1/named.bin"}]}]}`))
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
	f := newGenFixtureWithFreeBytes(t, false, func(string) (uint64, error) { return 1 << 20, nil }) // 1 MiB free
	data := bundleZip(t, map[string][]byte{"proj1/named.bin": bytes.Repeat([]byte{0}, 4<<20)})      // 4 MiB, tiny once deflated
	require.Less(t, len(data), 1<<20)

	var ve *service.ValidationError
	require.ErrorAs(t, importBundle(f, data), &ve)
	assert.Contains(t, ve.Message, "空き容量")

	_, err := os.Stat(filepath.Join(f.assetsDir, "proj1"))
	assert.True(t, os.IsNotExist(err), "no file was written")
	var n int
	require.NoError(t, f.db.Reader().QueryRow(`SELECT COUNT(*) FROM project WHERE id = 'proj1'`).Scan(&n))
	assert.Zero(t, n)

	roomy := newGenFixtureWithFreeBytes(t, false, func(string) (uint64, error) { return 8 << 20, nil })
	assert.NoError(t, importBundle(roomy, data), "the same bundle imports when the disk has room")
}

func TestRemoveStaleImports_RemovesOnlyInterruptedImportDirectories(t *testing.T) {
	assets := t.TempDir()
	stale := filepath.Join(assets, ".import-1234")
	require.NoError(t, os.MkdirAll(filepath.Join(stale, "proj1", "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "proj1", "sub", "big.bin"), []byte("x"), 0o600))
	keepDirs := []string{filepath.Join(assets, "proj1"), filepath.Join(assets, ".previews"), filepath.Join(assets, ".staging")}
	for _, d := range keepDirs {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	notDir := filepath.Join(assets, ".import-file")
	require.NoError(t, os.WriteFile(notDir, []byte("x"), 0o600))

	removed, err := service.RemoveStaleImports(assets)

	require.NoError(t, err)
	assert.Equal(t, []string{stale}, removed)
	assert.NoDirExists(t, stale)
	for _, d := range keepDirs {
		assert.DirExists(t, d)
	}
	assert.FileExists(t, notDir)
}

func TestRemoveStaleImports_MissingAssetsDirectoryIsNotAnError(t *testing.T) {
	removed, err := service.RemoveStaleImports(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Empty(t, removed)
}
