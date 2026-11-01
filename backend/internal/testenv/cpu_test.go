package testenv_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// spin does a fixed amount of CPU work (tens of milliseconds of it) on the calling goroutine. The
// amount of work, not a time, is fixed: a loop that runs for a while would do less on a busy machine.
func spin() {
	x := 0
	for i := 0; i < 2e8; i++ {
		x += i ^ (x >> 3)
	}
	sink = x
}

var sink int

func TestCPUTime_CountsTheWorkOfTheCaller(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux measures the CPU time of a thread; elsewhere CPUTime is the wall clock")
	}
	got := testenv.CPUTime(spin)
	assert.Greater(t, got, 10*time.Millisecond, "a busy loop uses CPU")
}

// A caller that is not running (descheduled on a busy machine, here blocked) is not charged: that
// is why a limit on CPUTime does not fail under load.
func TestCPUTime_DoesNotCountTheTimeTheCallerWasNotRunning(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux measures the CPU time of a thread; elsewhere CPUTime is the wall clock")
	}
	wait := make(chan struct{})
	time.AfterFunc(300*time.Millisecond, func() { close(wait) })
	got := testenv.CPUTime(func() { <-wait })
	assert.Less(t, got, 100*time.Millisecond, "300 ms passed but the caller did nothing")
}
