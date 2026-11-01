package testenv

import (
	"runtime"
	"time"
)

// calibrationRounds is how many times Starvation measures; the largest result counts, so a burst
// of load that starts right after a quiet moment still shows.
const calibrationRounds = 3

// Starvation says how much a busy machine stretches the clock for this process right now: 1 on an
// idle machine, 40 when a fixed piece of CPU work takes 40 times longer than the CPU it uses.
// A time limit on the clock can be multiplied by it (limit * Starvation()) so that a fast request
// does not fail only because the test process was not scheduled, while the limit stays exactly as
// strict as the clock on a machine that is not starved. Time spent waiting for a lock or for I/O
// is not starvation and is not forgiven: a request that waits on a lock while the machine is idle
// still fails. Measure before and after the request and take the larger of the two.
//
// Where the CPU time of a thread is not available (everywhere but Linux) it is 1: the limit is the
// clock.
func Starvation() float64 {
	f := 1.0
	for i := 0; i < calibrationRounds; i++ {
		f = max(f, stretch(calibrate))
	}
	return f
}

// calibrate is a fixed amount of CPU work (a few milliseconds). The amount of work, not a time, is
// fixed: a loop that runs for a while would do less on a busy machine and show nothing.
func calibrate() {
	x := 0
	for i := 0; i < 2e7; i++ {
		x += i ^ (x >> 3)
	}
	runtime.KeepAlive(x)
}

// stretch is how many times longer fn took than the CPU it used on its thread; at least 1.
func stretch(fn func()) float64 {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cpu0, wall0 := threadCPUTime(), time.Now()
	fn()
	cpu, wall := threadCPUTime()-cpu0, time.Since(wall0)
	if cpu <= 0 || wall <= cpu {
		return 1
	}
	return float64(wall) / float64(cpu)
}
