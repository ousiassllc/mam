package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Issue #1047: a mam that started with no ffmpeg runs it from the moment its path is saved, with no restart.
func TestFFmpegSetUpWhileMamRunsIsUsedAtOnce(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg, logger, ctx := testConfig(t), slog.New(slog.DiscardHandler), context.Background()
	dl := newFFmpegDownloader(cfg, logger)
	services, db, cleanup, err := buildServices(ctx, cfg, logger, testChatGPT(cfg), dl)
	require.NoError(t, err)
	t.Cleanup(func() { cleanup(); dl.Close() })
	_, err = services.Judge.Judge(ctx, "t1")
	require.ErrorIs(t, err, service.ErrUnavailable, "the Judge is off while ffmpeg is missing")
	paths := locate.Config{FFmpeg: filepath.Join(t.TempDir(), "ffmpeg"), FFprobe: filepath.Join(t.TempDir(), "ffprobe")}
	for _, exe := range []string{paths.FFmpeg, paths.FFprobe} {
		require.NoError(t, os.WriteFile(exe, nil, 0o700)) // #nosec G306 -- a test executable
	}
	require.NoError(t, newSettings(cfg, services, testChatGPT(cfg), dl, nil, repository.NewJobRepository(db), logger).FFmpeg.Set(paths))
	_, err = services.Judge.Judge(ctx, "t1")
	assert.NotErrorIs(t, err, service.ErrUnavailable, "the Judge is on now")
}
