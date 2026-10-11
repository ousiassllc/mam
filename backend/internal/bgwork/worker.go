// Package bgwork runs work on goroutines of its own so that a panic in it is logged instead of ending
// the server: a recovered one-off (RunSafely) and a worker that coalesces its wake-ups (Worker).
package bgwork

import (
	"context"
	"log/slog"
	"sync"
)

// Worker runs one function on ONE worker goroutine, whenever it is woken. Wake-ups coalesce: any
// number of Wake calls while a run is going on make exactly one more run. The goroutine starts at
// the first Wake and stops with Close. A run that panics is logged (RunSafely) and ends there: the
// worker waits for the next Wake.
type Worker struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger
	name   string
	run    func(ctx context.Context)
	wake   chan struct{} // capacity 1
	once   sync.Once
	done   chan struct{}
}

// NewWorker returns a Worker that calls run on each wake-up; name tells a panicking run in the log.
func NewWorker(logger *slog.Logger, name string, run func(ctx context.Context)) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{ctx: ctx, cancel: cancel, logger: logger, name: name, run: run, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// Wake asks for a run; it never blocks and does nothing after Close.
func (w *Worker) Wake() {
	if w.ctx.Err() != nil {
		return
	}
	w.once.Do(func() { go w.loop() })
	select {
	case w.wake <- struct{}{}:
	default: // a run is already asked for
	}
}

func (w *Worker) loop() {
	defer close(w.done)
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.wake:
			RunSafely(w.logger, w.name, func() { w.run(w.ctx) })
		}
	}
}

// Close stops the worker and waits for the run in progress.
func (w *Worker) Close() {
	w.cancel()
	started := true
	w.once.Do(func() { started = false }) // never started: nothing to wait for
	if started {
		<-w.done
	}
}
