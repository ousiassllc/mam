package bundlezip_test

import (
	"os"
	"path/filepath"
	"runtime"
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

// One extraction directory that cannot be removed must not leave the others behind (#680).
func TestRemoveStaleImports_AFailureDoesNotStopTheOthers(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not restrict this user")
	}
	assets := t.TempDir()
	stuck := filepath.Join(assets, ".import-1")
	other := filepath.Join(assets, ".import-2")
	require.NoError(t, os.MkdirAll(filepath.Join(stuck, "proj"), 0o755))
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.Chmod(stuck, 0o500)) // its content cannot be unlinked
	t.Cleanup(func() { _ = os.Chmod(stuck, 0o755) })

	removed, err := bundlezip.RemoveStaleImports(assets)

	require.Error(t, err)
	assert.Equal(t, []string{other}, removed)
	assert.NoDirExists(t, other)
	assert.DirExists(t, stuck)
}
