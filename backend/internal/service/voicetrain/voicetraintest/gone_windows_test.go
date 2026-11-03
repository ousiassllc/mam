//go:build windows

package voicetraintest_test

import (
	"math"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code that GetExitCodeProcess gives for a process that runs (STILL_ACTIVE, which x/sys/windows does not name).
const stillActive = 259

// processGone reports whether no process with the pid is running any more: one that cannot be opened is gone, and one that can be opened
// but has an exit code is a process that ended and whose handle someone still holds.
func processGone(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return true
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return true
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code != stillActive
}
