package exeinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

// Archive is one file that an install downloads: where it lies once downloaded and how large it is.
type Archive struct {
	Path string
	Size int64
}

// NeedBytes is the free space an install needs: the part of each archive that is not downloaded yet, and the extracted files
// (extracted bytes in all), which exist next to the archives until these are deleted. What of an archive is in place counts as
// downloaded: the part file of an interrupted download, or the whole archive an install that failed after the download kept
// (it is verified again before it is used).
func NeedBytes(extracted int64, archives ...Archive) int64 {
	need := extracted
	for _, a := range archives {
		have := int64(0)
		if st, err := os.Stat(fetch.PartPath(a.Path)); err == nil {
			have = min(st.Size(), a.Size)
		} else if st, err := os.Stat(a.Path); err == nil {
			have = min(st.Size(), a.Size)
		}
		need += a.Size - have
	}
	return need
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

// swapAttempts and swapDelay bound the retry of one rename in Swap: a rename can fail for a moment on Windows while a security
// product or the indexer holds a file of the directory open. The wait is at most (swapAttempts-1)*swapDelay for each rename.
const (
	swapAttempts = 5
	swapDelay    = 200 * time.Millisecond
)

// renamer is os.Rename, and the wait between two attempts is time.Sleep; a test replaces both.
type renamer struct {
	rename func(from, to string) error
	sleep  func(time.Duration)
}

var osRenamer = renamer{rename: os.Rename, sleep: time.Sleep}

// do renames from to to, trying again after a short wait while it fails; a missing from is not waited for. The last error is
// returned once swapAttempts attempts failed.
func (r renamer) do(from, to string) error {
	var err error
	for attempt := 1; attempt <= swapAttempts; attempt++ {
		if err = r.rename(from, to); err == nil || errors.Is(err, os.ErrNotExist) {
			return err
		}
		if attempt < swapAttempts {
			r.sleep(swapDelay)
		}
	}
	return fmt.Errorf("%w (failed %d times; another program may be using the files)", err, swapAttempts)
}

// Swap makes newDir the directory dir: the previous dir waits in oldDir, and is removed once the new one is in place (or put
// back when it cannot be). A rename that fails is tried again a few times, a short wait apart, before Swap gives up; the
// previous dir is then left (or put back) where it was.
func Swap(newDir, dir, oldDir string) error { return osRenamer.swap(newDir, dir, oldDir) }

func (r renamer) swap(newDir, dir, oldDir string) error {
	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(dir); err == nil {
		if err := r.do(dir, oldDir); err != nil {
			return fmt.Errorf("exeinstall: replace the installed copy: %w", err)
		}
	}
	if err := r.do(newDir, dir); err != nil {
		_ = r.do(oldDir, dir) // put the previous copy back
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
