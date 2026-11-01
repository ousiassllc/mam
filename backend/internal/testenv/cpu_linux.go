package testenv

import (
	"syscall"
	"time"
)

// rusageThread is RUSAGE_THREAD of getrusage(2): the syscall package defines only RUSAGE_SELF and
// RUSAGE_CHILDREN.
const rusageThread = 1

// threadCPUTime is the user and system CPU time the calling thread has used. The wall clock is not
// a fallback: a failure here makes the measurement meaningless, so it reports it.
func threadCPUTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(rusageThread, &ru); err != nil {
		panic("testenv: getrusage(RUSAGE_THREAD): " + err.Error())
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
