package app

import (
	"fmt"
	"os"
	"path/filepath"
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
