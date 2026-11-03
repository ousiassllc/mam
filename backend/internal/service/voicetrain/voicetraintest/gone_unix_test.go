//go:build !windows

package voicetraintest_test

import (
	"errors"
	"syscall"
)

// processGone reports whether no process with the pid exists any more (signal 0 only asks).
func processGone(pid int) bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }
