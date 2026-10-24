package modeladapter

import (
	"context"
	"errors"
	"time"
)

// limitClock is the time limit of one job (VideoJob.MaxTime) over all its model calls. It starts at
// the first vendor concurrency slot the job gets, not when the job asks for it: the time spent
// waiting for other jobs to free a slot is not model time. A nil clock, or one without a limit,
// never expires.
type limitClock struct {
	limit    time.Duration
	deadline time.Time
	// ctx is the context the limit applied to last; it tells an expired limit from a cancelled call.
	ctx context.Context
}

// start binds ctx to the limit, starting the clock if it has not started yet. The returned cancel
// releases the bound context.
func (c *limitClock) start(ctx context.Context) (context.Context, context.CancelFunc) {
	if c == nil || c.limit <= 0 {
		return ctx, func() {}
	}
	if c.deadline.IsZero() {
		c.deadline = time.Now().Add(c.limit)
	}
	bound, cancel := context.WithDeadline(ctx, c.deadline)
	c.ctx = bound
	return bound, cancel
}

// expired reports whether the limit ran out (rather than the caller giving up).
func (c *limitClock) expired() bool {
	return c != nil && c.ctx != nil && errors.Is(c.ctx.Err(), context.DeadlineExceeded) && !c.deadline.IsZero()
}
