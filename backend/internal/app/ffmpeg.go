package app

import (
	"log/slog"
	"runtime"

	"github.com/ousiassllc/mam/backend/internal/app/dlwire"
	"github.com/ousiassllc/mam/backend/internal/config"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
)

// newFFmpegDownloader is the download of ffmpeg and ffprobe (spec/architecture/ffmpeg-download.md): in the "ffmpeg" directory next to
// the executable on Windows, and in the data directory on macOS (ffmpegdl.For: the executable there is inside MAM.app, which the
// auto-update replaces). Where there is no pinned build for the platform, or the place cannot be found (no executable on Windows,
// no data directory on macOS), the download is off and the service is not Supported; an ffmpeg the user installed still works.
func newFFmpegDownloader(cfg config.Config, logger *slog.Logger) *ffmpegdl.Service {
	return ffmpegdl.New(ffmpegdl.Options{
		Layout: dlwire.FFmpeg(runtime.GOOS, executablePath, logger, cfg.DataDir), Catalog: ffmpegdl.Default(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Logger: logger,
	})
}

// newFFmpegSettings is the ffmpeg settings (ffmpeg.json) with the lookup that ends in the downloaded copy: it is searched after
// PATH and the Homebrew directories, so an ffmpeg the user installed always wins (spec/architecture/ffmpeg-lookup.md).
func newFFmpegSettings(cfg config.Config, dl *ffmpegdl.Service, opts ...ffmpegsettings.Option) *ffmpegsettings.Service {
	return ffmpegsettings.New(cfg.FFmpegSettingsFile(), append([]ffmpegsettings.Option{ffmpegsettings.WithManagedDirs(dl.Dirs)}, opts...)...)
}
