//go:build !windows

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // a file descriptor fits int
	if errors.Is(err, unix.EWOULDBLOCK) {
		return ErrDirLocked
	}
	return err
}

func unlockFile(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) } //nolint:gosec // a file descriptor fits int
