//go:build windows

package desktop

import (
	"math"
	"syscall"
	"time"
)

// waitForExit waits until the process pid has ended or limit has passed, and reports whether it ended. A process that
// cannot be opened is already gone (and so is a pid that is no process id at all).
func waitForExit(pid int, limit time.Duration) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return true
	}
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	ms := uint32(math.MaxUint32 - 1) // MaxUint32 is INFINITE: never wait without a limit
	if l := limit.Milliseconds(); l >= 0 && l < math.MaxUint32 {
		ms = uint32(l)
	}
	event, err := syscall.WaitForSingleObject(h, ms)
	return err == nil && event == syscall.WAIT_OBJECT_0
}
