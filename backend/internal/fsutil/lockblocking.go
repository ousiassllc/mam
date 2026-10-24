package fsutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// lockPollInterval is how often LockFileBlocking retries a lock another holder has.
const lockPollInterval = 25 * time.Millisecond

// FileLock is an exclusive lock on a file held until Unlock (or the process exits: the operating
// system releases it, so a crash never leaves a stale lock behind).
type FileLock struct {
	f *os.File
}

// LockFileBlocking creates the lock file at path (and its directory) when needed and waits until it
// holds an exclusive lock on it: unlike LockDir it does not give up when another process holds the lock.
// It returns ctx.Err() when ctx ends first. Different descriptors of the same process exclude each other
// on Unix and Windows alike, but a caller that needs to exclude goroutines cheaply should still hold a mutex first.
func LockFileBlocking(ctx context.Context, path string) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the caller names its own lock file
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	for {
		err := lockFile(f)
		if err == nil {
			return &FileLock{f: f}, nil
		}
		if !errors.Is(err, ErrDirLocked) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}
}

// Unlock releases the lock. It is safe to call more than once.
func (l *FileLock) Unlock() {
	if l == nil || l.f == nil {
		return
	}
	unlockFile(l.f)
	_ = l.f.Close()
	l.f = nil
}
