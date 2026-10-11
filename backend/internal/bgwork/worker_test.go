package bgwork_test

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/bgwork"
)

func TestWorker_WakesCoalesceIntoOneRunAtATime(t *testing.T) {
	var running, peak, runs atomic.Int32
	started := make(chan struct{}, 8)
	gate := make(chan struct{})
	r := bgwork.NewWorker(slog.New(slog.DiscardHandler), "test", func(context.Context) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		runs.Add(1)
		started <- struct{}{}
		<-gate
		running.Add(-1)
	})
	defer r.Close()

	r.Wake()
	<-started // the first run is going on
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() { defer wg.Done(); r.Wake() }()
	}
	wg.Wait()
	gate <- struct{}{} // finish the first run; the wakes that came meanwhile make one more
	<-started
	gate <- struct{}{}

	require.Eventually(t, func() bool { return running.Load() == 0 }, time.Second, time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(2), runs.Load(), "200 wakes during a run are one more run")
	assert.Equal(t, int32(1), peak.Load(), "runs never overlap")
}

func TestWorker_CloseStopsTheWorkerAndLaterWakesDoNothing(t *testing.T) {
	var runs atomic.Int32
	r := bgwork.NewWorker(slog.New(slog.DiscardHandler), "test", func(context.Context) { runs.Add(1) })
	r.Close() // never started: returns at once

	r.Wake()
	time.Sleep(20 * time.Millisecond)
	assert.Zero(t, runs.Load())

	r = bgwork.NewWorker(slog.New(slog.DiscardHandler), "test", func(context.Context) { runs.Add(1) })
	r.Wake()
	require.Eventually(t, func() bool { return runs.Load() == 1 }, time.Second, time.Millisecond)
	r.Close()
	r.Wake()
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, int32(1), runs.Load())
}

func TestWorker_APanickingRunIsLoggedAndTheWorkerGoesOn(t *testing.T) {
	var runs atomic.Int32
	r := bgwork.NewWorker(slog.New(slog.DiscardHandler), "test", func(context.Context) {
		runs.Add(1)
		panic("boom")
	})
	defer r.Close()

	r.Wake()
	require.Eventually(t, func() bool { return runs.Load() == 1 }, time.Second, time.Millisecond)
	r.Wake()
	require.Eventually(t, func() bool { return runs.Load() == 2 }, time.Second, time.Millisecond, "the worker survives the panic")
}

func TestRunSafely_LogsThePanicWithItsStack(t *testing.T) {
	var buf bytes.Buffer
	bgwork.RunSafely(slog.New(slog.NewTextHandler(&buf, nil)), "some work", func() { panic("boom") })
	assert.Contains(t, buf.String(), "some work panicked")
	assert.Contains(t, buf.String(), "boom")
	assert.Contains(t, buf.String(), "stack=")
}

func TestAsync_RunsOnItsOwnGoroutineAndSurvivesAPanic(t *testing.T) {
	got := make(chan int, 1)
	run := bgwork.Async(slog.New(slog.DiscardHandler), "some loop", func(n int) {
		if n == 0 {
			panic("boom") // an unrecovered panic here would end the test binary
		}
		got <- n
	})
	run(0)
	run(7)
	assert.Equal(t, 7, <-got)
}
