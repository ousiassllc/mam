package ffwire_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/ffwire"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

var logger = slog.New(slog.DiscardHandler)

// install puts an executable called name (with the suffix of the OS) into dir.
func install(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o700)) // #nosec G306 -- a test executable
	return p
}

func lookIn(dir string) func() (media.Tools, error) {
	finder := locate.Finder{GOOS: runtime.GOOS, Path: dir, Usable: locate.IsExecutable}
	return func() (media.Tools, error) { return media.LookIn(finder, locate.Config{}) }
}

// Reload hands what the lookup finds to the Tools; a lookup that finds nothing empties them, and the next one that finds it fills them again.
func TestReload(t *testing.T) {
	tools, dir := media.NewTools(), t.TempDir()

	ffwire.Reload(&tools, lookIn(dir), logger)
	assert.False(t, tools.Available())

	ffmpeg := install(t, dir, "ffmpeg")
	install(t, dir, "ffprobe")
	ffwire.Reload(&tools, lookIn(dir), logger)
	assert.True(t, tools.Available())
	assert.Equal(t, ffmpeg, tools.FFmpegPath())

	require.NoError(t, os.Remove(ffmpeg))
	ffwire.Reload(&tools, lookIn(dir), logger)
	assert.False(t, tools.Available(), "ffmpeg that was removed is not run any more")
}

// Shown makes the services run what the screen shows: ffmpeg that appeared where the search looks is taken at once, one that went is dropped,
// and a view that agrees with the Tools changes nothing.
func TestShown(t *testing.T) {
	tools, dir := media.NewTools(), t.TempDir()
	shown := ffwire.Shown(&tools, lookIn(dir), logger)

	shown("", "")
	assert.False(t, tools.Available(), "nothing is shown, nothing runs")

	ffmpeg, ffprobe := install(t, dir, "ffmpeg"), install(t, dir, "ffprobe")
	shown(ffmpeg, ffprobe)
	assert.True(t, tools.Available(), "what appeared is shown, so it runs")
	assert.Equal(t, ffprobe, tools.FFprobePath())

	require.NoError(t, os.Remove(ffprobe))
	shown(ffmpeg, "")
	assert.False(t, tools.Available(), "what went is not run any more")
}
