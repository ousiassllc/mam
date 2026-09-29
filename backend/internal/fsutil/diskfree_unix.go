//go:build !windows

package fsutil

import (
	"fmt"
	"path/filepath"
	"syscall"
)

func parentOf(path string) string { return filepath.Dir(path) }

func freeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return st.Bavail * uint64(st.Bsize), nil //nolint:gosec // Bsize is positive
}
