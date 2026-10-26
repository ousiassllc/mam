package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

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

// codexLayout is where the Codex app-server of the "Codex 経由" ChatGPT login is downloaded to: the "codex" directory
// next to the executable. When the executable cannot be found the layout is empty, which leaves the download off (a path
// to a codex the user installed still works).
func codexLayout(logger *slog.Logger) engine.Layout {
	exe, err := executablePath()
	if err != nil {
		logger.Warn("the Codex engine download is off", "err", err)
		return engine.Layout{}
	}
	return engine.ForExecutable(exe)
}
