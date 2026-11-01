package exeinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// RootNextToExecutable is the directory called name next to the executable at exe.
func RootNextToExecutable(exe, name string) string { return filepath.Join(filepath.Dir(exe), name) }

// RootInDataDir is the directory called name in the data directory. An empty dataDir gives "" (no download).
func RootInDataDir(dataDir, name string) string {
	if dataDir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	return filepath.Join(dataDir, name)
}

// Root is where a downloaded program called name lies on goos. On macOS it lies in the data directory, outside MAM.app: the
// executable sits inside the .app, which the auto-update replaces as a whole, and anything put there would go with it.
// Elsewhere it lies next to the executable at exe (on Windows the uninstaller removes the install directory, so it removes
// the program too).
func Root(goos, exe, dataDir, name string) string {
	if goos == "darwin" {
		return RootInDataDir(dataDir, name)
	}
	return RootNextToExecutable(exe, name)
}

// CheckSpace returns a *fetch.NoSpaceError when the disk at root cannot hold need bytes. free reports the free space at a path
// (nil: fsutil.FreeBytes).
func CheckSpace(root string, need int64, free func(path string) (uint64, error)) error {
	if free == nil {
		free = fsutil.FreeBytes
	}
	have, err := free(root)
	if err != nil {
		return fmt.Errorf("free space: %w", err)
	}
	if uint64(need) > have { //nolint:gosec // need is a size, never negative
		return &fetch.NoSpaceError{Need: uint64(need), Free: have, Path: root} //nolint:gosec // same
	}
	return nil
}

// Swap makes newDir the directory dir: the previous dir waits in oldDir, and is removed once the new one is in place (or put
// back when it cannot be).
func Swap(newDir, dir, oldDir string) error {
	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, oldDir); err != nil {
			return fmt.Errorf("exeinstall: replace the installed copy: %w", err)
		}
	}
	if err := os.Rename(newDir, dir); err != nil {
		_ = os.Rename(oldDir, dir) // put the previous copy back
		return fmt.Errorf("exeinstall: put the new copy in place: %w", err)
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

// CleanLeftovers removes what an interrupted install left: a half-extracted newDir, and oldDir (or puts it back as dir when the
// swap stopped between its two renames).
func CleanLeftovers(newDir, dir, oldDir string) {
	_ = os.RemoveAll(newDir)
	if _, err := os.Stat(oldDir); err != nil {
		return
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		_ = os.Rename(oldDir, dir)
		return
	}
	_ = os.RemoveAll(oldDir)
}
