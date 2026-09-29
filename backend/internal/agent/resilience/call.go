package resilience

import (
	"context"
	"errors"
	"time"
)

// ErrOpen marks a call that was not made because the provider's circuit breaker is open.
var ErrOpen = errors.New("circuit breaker is open")

// Policy is the retry schedule: at most MaxAttempts calls, waiting BaseDelay before the
// second one and doubling up to MaxDelay.
type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// Hooks tell Do how to judge an error and how to wait.
type Hooks struct {
	// Unavailable reports whether err says the provider is temporarily unusable. Only those
	// errors are retried and counted by the breaker.
	Unavailable func(error) bool
	// Retryable, when set, narrows which Unavailable errors are tried again (all of them when nil).
	// The others still count for the breaker.
	Retryable func(error) bool
	// RetryAfter is the wait a provider asked for (429 Retry-After); zero when none.
	RetryAfter func(error) time.Duration
	// Sleep waits for d or until ctx ends.
	Sleep func(ctx context.Context, d time.Duration) error
	// OnRetry, when set, is told about every retry before the wait.
	OnRetry func(attempt int, wait time.Duration, err error)
}

// SleepContext waits for d or until ctx ends.
func SleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Do calls fn, retrying failures that Unavailable accepts with exponential backoff. The breaker
// is consulted before every attempt: once it is open no further attempt is made, and Do returns
// the last failure (ErrOpen when the breaker was open before the first attempt).
func Do[T any](ctx context.Context, b *Breaker, p Policy, h Hooks, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	var last error
	delay := p.BaseDelay
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if !b.Allow() {
			if last != nil {
				return zero, last // the failures of this call opened the breaker
			}
			return zero, ErrOpen
		}
		out, err := fn(ctx)
		switch {
		case err == nil:
			b.Success()
			return out, nil
		case ctx.Err() != nil:
			b.Release()
			return zero, ctx.Err()
		case !h.Unavailable(err):
			b.Release()
			return zero, err
		}
		b.Failure()
		last = err
		if attempt == p.MaxAttempts || b.IsOpen() || (h.Retryable != nil && !h.Retryable(err)) {
			break
		}
		wait := min(max(delay, h.RetryAfter(err)), p.MaxDelay)
		if h.OnRetry != nil {
			h.OnRetry(attempt, wait, err)
		}
		if err := h.Sleep(ctx, wait); err != nil {
			return zero, err
		}
		delay = min(delay*2, p.MaxDelay)
	}
	return zero, last
}
