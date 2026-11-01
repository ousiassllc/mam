package exeinstall_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func TestRootPlacesTheProgramByOS(t *testing.T) {
	exe := filepath.Join("Applications", "MAM.app", "Contents", "MacOS", "mam")
	data := t.TempDir()
	assert.Equal(t, filepath.Join(data, "tool"), exeinstall.Root("darwin", exe, data, "tool"))
	assert.False(t, strings.Contains(exeinstall.Root("darwin", exe, data, "tool"), ".app"), "outside MAM.app: the update replaces the bundle")
	for _, goos := range []string{"windows", "linux"} {
		assert.Equal(t, filepath.Join("x", "tool"), exeinstall.Root(goos, filepath.Join("x", "mam.exe"), data, "tool"), goos)
	}
	assert.Empty(t, exeinstall.Root("darwin", exe, "", "tool"), "no data directory: no download")
	rel := exeinstall.Root("darwin", exe, "data", "tool")
	assert.True(t, filepath.IsAbs(rel), "a relative data directory gives %q, want an absolute path", rel)
}

func TestCheckSpaceNamesWhatIsMissing(t *testing.T) {
	free := func(string) (uint64, error) { return 100, nil }
	require.NoError(t, exeinstall.CheckSpace("r", 100, free))
	var noSpace *fetch.NoSpaceError
	require.ErrorAs(t, exeinstall.CheckSpace("r", 101, free), &noSpace)
	assert.Equal(t, uint64(101), noSpace.Need)
	assert.Equal(t, uint64(100), noSpace.Free)
	boom := errors.New("boom")
	require.ErrorIs(t, exeinstall.CheckSpace("r", 1, func(string) (uint64, error) { return 0, boom }), boom)
}

func mkdirFile(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte(content), 0o600))
}

func read(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "f"))
	require.NoError(t, err)
	return string(b)
}

func TestSwapReplacesTheCopyAndLeavesNoOldOne(t *testing.T) {
	root := t.TempDir()
	dir, newDir, oldDir := filepath.Join(root, "bin"), filepath.Join(root, "bin.new"), filepath.Join(root, "bin.old")
	mkdirFile(t, dir, "old")
	mkdirFile(t, newDir, "new")
	require.NoError(t, exeinstall.Swap(newDir, dir, oldDir))
	assert.Equal(t, "new", read(t, dir))
	for _, gone := range []string{newDir, oldDir} {
		_, err := os.Stat(gone)
		assert.Error(t, err, gone)
	}
	// A first install has nothing to move aside.
	require.NoError(t, os.RemoveAll(dir))
	mkdirFile(t, newDir, "first")
	require.NoError(t, exeinstall.Swap(newDir, dir, oldDir))
	assert.Equal(t, "first", read(t, dir))
}

func TestSwapPutsThePreviousCopyBackWhenTheNewOneCannotBePlaced(t *testing.T) {
	root := t.TempDir()
	dir, oldDir := filepath.Join(root, "bin"), filepath.Join(root, "bin.old")
	mkdirFile(t, dir, "old")
	require.Error(t, exeinstall.Swap(filepath.Join(root, "missing"), dir, oldDir))
	assert.Equal(t, "old", read(t, dir))
}

func TestCleanLeftoversRestoresOrRemoves(t *testing.T) {
	root := t.TempDir()
	dir, newDir, oldDir := filepath.Join(root, "bin"), filepath.Join(root, "bin.new"), filepath.Join(root, "bin.old")
	// The swap stopped between its two renames: dir is missing, oldDir holds the copy; newDir is half extracted.
	mkdirFile(t, oldDir, "kept")
	mkdirFile(t, newDir, "half")
	exeinstall.CleanLeftovers(newDir, dir, oldDir)
	assert.Equal(t, "kept", read(t, dir))
	for _, gone := range []string{newDir, oldDir} {
		_, err := os.Stat(gone)
		assert.Error(t, err, gone)
	}
	// A stale oldDir beside a dir is removed.
	mkdirFile(t, oldDir, "stale")
	exeinstall.CleanLeftovers(newDir, dir, oldDir)
	_, err := os.Stat(oldDir)
	assert.Error(t, err)
	assert.Equal(t, "kept", read(t, dir))
}
