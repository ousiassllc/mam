package dlwire_test

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/app/dlwire"
)

func noExecutable() (string, error) { return "", errors.New("find the executable: no such file") }

// On macOS the downloaded engines lie in the data directory, so an executable that cannot be found does not matter (Issue #1055).
func TestDownloadLayout_DarwinDoesNotNeedTheExecutable(t *testing.T) {
	dataDir := t.TempDir()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	codex := dlwire.Codex("darwin", noExecutable, logger, dataDir)
	ffmpeg := dlwire.FFmpeg("darwin", noExecutable, logger, dataDir)

	assert.Equal(t, filepath.Join(dataDir, "codex"), codex.Root)
	assert.Equal(t, filepath.Join(dataDir, "ffmpeg"), ffmpeg.Root)
	assert.Empty(t, logs.String(), "the download is on: nothing to warn about")
}

// Elsewhere the engines lie next to the executable: without it the download is off, and the log says why.
func TestDownloadLayout_WindowsWithoutTheExecutableIsOff(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	assert.Empty(t, dlwire.Codex("windows", noExecutable, logger, t.TempDir()).Root)
	assert.Contains(t, logs.String(), "the Codex engine download is off")
	assert.Empty(t, dlwire.FFmpeg("windows", noExecutable, logger, t.TempDir()).Root)
	assert.Contains(t, logs.String(), "the ffmpeg download is off")
}

func TestDownloadLayout_WindowsLiesNextToTheExecutable(t *testing.T) {
	exe := func() (string, error) { return filepath.Join("C:", "mam", "mam.exe"), nil }

	codex := dlwire.Codex("windows", exe, slog.New(slog.DiscardHandler), t.TempDir())
	ffmpeg := dlwire.FFmpeg("windows", exe, slog.New(slog.DiscardHandler), t.TempDir())

	assert.Equal(t, filepath.Join("C:", "mam", "codex"), codex.Root)
	assert.Equal(t, filepath.Join("C:", "mam", "ffmpeg"), ffmpeg.Root)
}

// With no data directory on macOS there is no place for a download: it stays off, as before.
func TestDownloadLayout_DarwinWithoutADataDirectoryIsOff(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	exe := func() (string, error) { return "/Applications/MAM.app/Contents/MacOS/mam", nil }

	require.Empty(t, dlwire.Codex("darwin", exe, logger, "").Root)
	require.Empty(t, dlwire.FFmpeg("darwin", exe, logger, "").Root)
}
