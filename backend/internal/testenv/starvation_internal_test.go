package testenv

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A caller that is not running is what starvation looks like from inside the process: the wall
// clock runs, its CPU does not. Here it is blocked, which is the same to the thread's clocks.
func TestStretch_ACallerThatDidNotRunIsStretched(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux measures the CPU time of a thread; elsewhere the stretch is 1")
	}
	got := stretch(func() { time.Sleep(200 * time.Millisecond) })
	assert.Greater(t, got, 20.0, "200 ms passed, almost no CPU was used")
}

func TestStretch_IsNeverBelowOne(t *testing.T) {
	assert.GreaterOrEqual(t, stretch(func() {}), 1.0)
	assert.GreaterOrEqual(t, Starvation(), 1.0)
}
