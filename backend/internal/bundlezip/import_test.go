package bundlezip_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/bundlezip"
)

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

	removed, err := bundlezip.RemoveStaleImports(assets)

	require.NoError(t, err)
	assert.Equal(t, []string{stale}, removed)
	assert.NoDirExists(t, stale)
	for _, d := range keepDirs {
		assert.DirExists(t, d)
	}
	assert.FileExists(t, notDir)
}

func TestRemoveStaleImports_MissingAssetsDirectoryIsNotAnError(t *testing.T) {
	removed, err := bundlezip.RemoveStaleImports(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Empty(t, removed)
}
