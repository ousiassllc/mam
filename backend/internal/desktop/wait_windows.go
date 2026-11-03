//go:build windows

package desktop

import (
	"syscall"
	"time"
)

// waitForExit waits until the process pid has ended or limit has passed, and reports whether it ended. A process that
// cannot be opened is already gone.
func waitForExit(pid int, limit time.Duration) bool {
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	event, err := syscall.WaitForSingleObject(h, uint32(limit/time.Millisecond))
	return err == nil && event == syscall.WAIT_OBJECT_0
}
