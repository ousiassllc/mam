package fsutil_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

func TestWriteFileAtomic_ReplacesContentAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "take.mp4")

	require.NoError(t, fsutil.WriteFileAtomic(path, []byte("v1"), 0o644))
	require.NoError(t, fsutil.WriteFileAtomic(path, []byte("v2"), 0o644))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "v2", string(got))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp file may remain")
}

func TestWriteAtomic_FailureKeepsOriginalAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "take.mp4")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o644))

	boom := errors.New("network dropped")
	err := fsutil.WriteAtomic(path, 0o644, func(w io.Writer) error {
		_, _ = w.Write([]byte("partial"))
		return boom
	})
	assert.ErrorIs(t, err, boom)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "original", string(got), "a failed write must not expose partial content")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestWriteAtomic_MissingDirectoryFails(t *testing.T) {
	err := fsutil.WriteFileAtomic(filepath.Join(t.TempDir(), "nope", "f"), []byte("x"), 0o644)
	assert.Error(t, err)
}

func TestRemoveStaleTemp_OnlyRemovesTempFiles(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "assets")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	stale := filepath.Join(sub, ".take.mp4.123.tmp")
	keep := []string{filepath.Join(sub, "take.mp4"), filepath.Join(sub, "notes.tmp"), filepath.Join(sub, ".hidden")}
	require.NoError(t, os.WriteFile(stale, []byte("x"), 0o644))
	for _, p := range keep {
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	}

	removed, err := fsutil.RemoveStaleTemp(root)
	require.NoError(t, err)
	assert.Equal(t, []string{stale}, removed)
	for _, p := range keep {
		assert.FileExists(t, p)
	}
}

func TestRemoveStaleTemp_MissingRootIsNotAnError(t *testing.T) {
	removed, err := fsutil.RemoveStaleTemp(filepath.Join(t.TempDir(), "absent"))
	assert.NoError(t, err)
	assert.Empty(t, removed)
}

// The conversions media makes in .previews (".<name>.ffmpeg.tmp") are swept by this alone (#667, #671).
func TestRemoveStaleTemp_RemovesAnFFmpegOutputLeftByAKilledConversion(t *testing.T) {
	root := t.TempDir()
	previews := filepath.Join(root, ".previews")
	require.NoError(t, os.MkdirAll(previews, 0o700))
	stale := filepath.Join(previews, ".x.mp4.ffmpeg.tmp")
	done := filepath.Join(previews, "x.mp4")
	require.NoError(t, os.WriteFile(stale, []byte("half"), 0o600))
	require.NoError(t, os.WriteFile(done, []byte("done"), 0o600))

	removed, err := fsutil.RemoveStaleTemp(root)

	require.NoError(t, err)
	assert.Equal(t, []string{stale}, removed)
	assert.FileExists(t, done)
}

func TestRemoveStaleTemp_RemovesAStagingFileLeftByACrash(t *testing.T) {
	root := t.TempDir()
	f, err := fsutil.CreateStagingTemp(filepath.Join(root, ".staging"))
	if err != nil {
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".staging"), 0o700))
		f, err = fsutil.CreateStagingTemp(filepath.Join(root, ".staging"))
	}
	require.NoError(t, err)
	require.NoError(t, f.Close())

	removed, err := fsutil.RemoveStaleTemp(root)

	require.NoError(t, err)
	assert.Equal(t, []string{f.Name()}, removed)
}
