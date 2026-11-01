package testenv

import (
	"runtime"
	"time"
)

// CPUTime runs fn and returns the CPU time it used, not the time that passed. A time limit on the
// wall clock fails a test that is fast but was not scheduled: with many test processes on one
// machine a request that takes 50 ms of CPU can take seconds to finish, and that says nothing about
// the code. fn must do its work on the calling goroutine (a synchronous request does): work it
// hands to another goroutine is not counted.
//
// Where the CPU time of a thread is not available (everywhere but Linux) it returns the wall-clock
// time, so the limit is then as strict as the clock.
func CPUTime(fn func()) time.Duration {
	runtime.LockOSThread() // the thread's clock is only meaningful while fn stays on the thread
	defer runtime.UnlockOSThread()
	start := threadCPUTime()
	fn()
	return threadCPUTime() - start
}
