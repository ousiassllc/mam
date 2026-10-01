package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDirLocked is returned by LockDir when another process holds the directory.
var ErrDirLocked = errors.New("directory is in use by another process")

// dirLockName is the lock file inside the locked directory.
const dirLockName = ".mam.lock"

// DirLock is an exclusive lock on a directory held until Unlock (or the process exits: the
// operating system releases it, so a crash never leaves a stale lock behind).
type DirLock struct {
	f *os.File
}

// LockDir creates dir when needed and takes an exclusive, non-blocking lock on it. It returns
// ErrDirLocked (wrapped) when another process holds the lock. Whoever owns the directory may then
// clean up leftovers of an interrupted run without touching the work of a running instance.
func LockDir(dir string) (*DirLock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, dirLockName), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // inside the data directory
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, ErrDirLocked) {
			return nil, fmt.Errorf("%s: %w", dir, ErrDirLocked)
		}
		return nil, fmt.Errorf("lock %s: %w", dir, err)
	}
	return &DirLock{f: f}, nil
}

// Unlock releases the lock. It is safe to call more than once.
func (l *DirLock) Unlock() {
	if l == nil || l.f == nil {
		return
	}
	unlockFile(l.f)
	_ = l.f.Close()
	l.f = nil
}
