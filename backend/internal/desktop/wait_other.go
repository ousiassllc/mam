//go:build !windows

package desktop

import (
	"errors"
	"syscall"
	"time"
)

// waitForExit waits until the process pid has ended or limit has passed, and reports whether it ended. It polls: a process
// that is not a child cannot be waited for with a handle off Windows.
func waitForExit(pid int, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
