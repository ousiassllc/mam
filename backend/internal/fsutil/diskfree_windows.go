//go:build windows

package fsutil

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func parentOf(path string) string { return filepath.Dir(path) }

func freeBytes(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, fmt.Errorf("get disk free space %s: %w", path, err)
	}
	return free, nil
}
