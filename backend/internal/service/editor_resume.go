package service

import (
	"context"
	"sync"
)

// resumer runs the follow-ups of a finished job or a selected Take (the return of the Reshoot
// Loop, the AUTO editing chain) on ONE worker goroutine. Wake-ups coalesce: any number of Wake calls
// while a run is going on make exactly one more run. The worker starts at the first Wake and stops
// with its context.
type resumer struct {
	ctx    context.Context
	cancel context.CancelFunc
	run    func(ctx context.Context)
	wake   chan struct{} // capacity 1
	once   sync.Once
	done   chan struct{}
}

func newResumer(run func(ctx context.Context)) *resumer {
	ctx, cancel := context.WithCancel(context.Background())
	return &resumer{ctx: ctx, cancel: cancel, run: run, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// Wake asks for a run; it never blocks and does nothing after Close.
func (r *resumer) Wake() {
	if r.ctx.Err() != nil {
		return
	}
	r.once.Do(func() { go r.loop() })
	select {
	case r.wake <- struct{}{}:
	default: // a run is already asked for
	}
}

func (r *resumer) loop() {
	defer close(r.done)
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.wake:
			r.run(r.ctx)
		}
	}
}

// Close stops the worker and waits for the run in progress.
func (r *resumer) Close() {
	r.cancel()
	started := true
	r.once.Do(func() { started = false }) // never started: nothing to wait for
	if started {
		<-r.done
	}
}

// ResumeEditing asks the worker to look for editing follow-ups: additional Shots of the Reshoot Loop
// that are ready to go into their Edit Timeline, and AUTO Projects whose editing stage can start.
// Call it whenever a job finished or a Take was selected.
func (s *EditorService) ResumeEditing() { s.resume.Wake() }

// Close stops the background worker of ResumeEditing.
func (s *EditorService) Close() { s.resume.Close() }

func (s *EditorService) resumeAll(ctx context.Context) {
	s.resumeReshoots(ctx)
	s.resumeAuto(ctx)
}
