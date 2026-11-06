package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
)

// executablePath is the running mam executable with its symbolic links resolved: the engines that mam downloads lie
// next to it, so that the uninstaller, which removes the install directory, removes them too.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find the executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// downloadAnchor is the executable that the place of a downloaded program is derived from (exeinstall.Root). On macOS the program
// lies in the data directory and the executable plays no part, so not finding it is no reason to switch the download off: the
// anchor is empty then. Elsewhere the program lies next to the executable, and not finding it is an error.
func downloadAnchor(goos string, find func() (string, error)) (string, error) {
	exe, err := find()
	if err != nil && goos == "darwin" {
		return "", nil
	}
	return exe, err
}

// codexLayout is where the Codex app-server of the "Codex 経由" ChatGPT login is downloaded to: the "codex" directory
// next to the executable on Windows, and in the data directory on macOS (engine.For: the executable there is inside
// MAM.app, which the auto-update replaces). When the place cannot be found (no executable on Windows, no data directory on
// macOS) the layout is empty, which leaves the download off (a path to a codex the user installed still works).
func codexLayout(logger *slog.Logger, dataDir string) engine.Layout {
	return codexLayoutFor(runtime.GOOS, executablePath, logger, dataDir)
}

func codexLayoutFor(goos string, find func() (string, error), logger *slog.Logger, dataDir string) engine.Layout {
	exe, err := downloadAnchor(goos, find)
	if err != nil {
		logger.Warn("the Codex engine download is off", "err", err)
		return engine.Layout{}
	}
	return engine.For(goos, exe, dataDir)
}
