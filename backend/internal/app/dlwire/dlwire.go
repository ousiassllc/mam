// Package dlwire finds the place of the programs that mam downloads (the Codex engine, ffmpeg) for the app. It is a package of its own, under app, so that app stays within its size (linterly).
package dlwire

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
)

// anchor is the executable that the place of a downloaded program is derived from (exeinstall.Root). On macOS the program
// lies in the data directory and the executable plays no part, so not finding it is no reason to switch the download off: the
// anchor is empty then. Elsewhere the program lies next to the executable, and not finding it is an error.
func anchor(goos string, find func() (string, error)) (string, error) {
	exe, err := find()
	if err != nil && goos == "darwin" {
		return "", nil
	}
	return exe, err
}

// Codex is where the Codex app-server of the "Codex 経由" ChatGPT login is downloaded to on the operating system goos: the "codex"
// directory next to the executable (find) on Windows, and in the data directory on macOS (engine.For: the executable there is
// inside MAM.app, which the auto-update replaces). When the place cannot be found (no executable on Windows, no data directory on
// macOS) the layout is empty, which leaves the download off (a path to a codex the user installed still works).
func Codex(goos string, find func() (string, error), logger *slog.Logger, dataDir string) engine.Layout {
	exe, err := anchor(goos, find)
	if err != nil {
		logger.Warn("the Codex engine download is off", "err", err)
		return engine.Layout{}
	}
	return engine.For(goos, exe, dataDir)
}

// FFmpeg is where ffmpeg and ffprobe are downloaded to on the operating system goos, found as Codex is (ffmpegdl.For); an empty
// layout leaves the download off.
func FFmpeg(goos string, find func() (string, error), logger *slog.Logger, dataDir string) ffmpegdl.Layout {
	exe, err := anchor(goos, find)
	if err != nil {
		logger.Warn("the ffmpeg download is off", "err", err)
		return ffmpegdl.Layout{}
	}
	return ffmpegdl.For(goos, exe, dataDir)
}
