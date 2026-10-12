package previewcache_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/previewcache"
)

// write makes a file of n bytes whose modification time is age ago.
func write(t *testing.T, dir, name string, n int, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, make([]byte, n), 0o600))
	at := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(p, at, at))
	return p
}

func newDirCache(t *testing.T, assets string, maxBytes int64) *previewcache.Cache {
	t.Helper()
	c := previewcache.New(slog.New(slog.NewTextHandler(io.Discard, nil)), assets, maxBytes)
	t.Cleanup(c.Close)
	return c
}

func TestPath_StaysDirectlyInsideTheDirectory(t *testing.T) {
	assets := t.TempDir()
	dir := filepath.Join(assets, ".previews")
	c := newDirCache(t, assets, 0)

	assert.Equal(t, filepath.Join(dir, "0b9f1a52.mp4"), c.Path("0b9f1a52.mp4"))
	for _, name := range []string{"a/../../x.mp4", "../x.mp4", "../../../x.mp4", "sub/x.mp4", "edit-a/../../../x.mp4", "", ".", ".."} {
		assert.Empty(t, c.Path(name), name)
	}
	assert.Empty(t, newDirCache(t, "", 0).Path("x.mp4"), "a cache without a directory makes nothing")
}

func TestSize_CountsFinishedPreviewsAndAMissingDirIsEmpty(t *testing.T) {
	assets := t.TempDir()
	dir := filepath.Join(assets, ".previews")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	write(t, dir, "a.mp4", 10, time.Hour)
	write(t, dir, "b.jpg", 5, time.Hour)
	write(t, dir, ".c.mp4.ffmpeg.tmp", 100, time.Hour) // a conversion in progress

	n, err := newDirCache(t, assets, 0).Size()
	require.NoError(t, err)
	assert.EqualValues(t, 15, n)

	n, err = newDirCache(t, filepath.Join(assets, "missing"), 0).Size()
	require.NoError(t, err)
	assert.Zero(t, n)
}

// Every Edit Timeline version makes its own Rough Cut and an old one is never played again: once a
// file is made, the oldest ones beyond the limit are deleted, and the one just made stays.
func TestMake_DeletesTheOldestFilesBeyondTheLimitButNotTheNewOneNorOnesInProgress(t *testing.T) {
	assets := t.TempDir()
	dir := filepath.Join(assets, ".previews")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	oldest := write(t, dir, "oldest.mp4", 10, 3*time.Hour)
	middle := write(t, dir, "middle.mp4", 10, 2*time.Hour)
	tmp := write(t, dir, ".busy.mp4.ffmpeg.tmp", 10, 5*time.Hour) // ffmpeg is writing it
	c := newDirCache(t, assets, 20)
	dst := c.Path("new.mp4")

	err := c.Make(context.Background(), dst, 0, func(context.Context) error { return os.WriteFile(dst, make([]byte, 10), 0o600) })

	require.NoError(t, err)
	assert.NoFileExists(t, oldest)
	assert.FileExists(t, middle)
	assert.FileExists(t, dst, "the one just made stays")
	assert.FileExists(t, tmp)

	// Even when it is the only one over the limit, the one just made is not deleted.
	big := c.Path("big.mp4")
	require.NoError(t, c.Make(context.Background(), big, 0, func(context.Context) error { return os.WriteFile(big, make([]byte, 50), 0o600) }))
	assert.FileExists(t, big)
	assert.NoFileExists(t, middle)
	assert.NoFileExists(t, dst)
}
