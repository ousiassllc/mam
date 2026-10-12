package resilience

import (
	"context"
	"errors"
	"time"
)

// JobRetryMaxDelay is the longest wait Repeat puts between two tries of a request for a job that
// the vendor accepted already.
const JobRetryMaxDelay = time.Minute

// Backoff is the wait schedule of Repeat: Base before the second try, doubling up to Max (never
// below Base).
type Backoff struct {
	Base time.Duration
	Max  time.Duration
}

type waitError struct {
	err   error
	after time.Duration
}

func (e *waitError) Error() string { return e.err.Error() }
func (e *waitError) Unwrap() error { return e.err }

// WithRetryAfter attaches the wait a vendor asked for (its Retry-After header) to err; Repeat honours
// it when Hooks.RetryAfter is unset. The error stays reachable through errors.As.
func WithRetryAfter(err error, after time.Duration) error { return &waitError{err: err, after: after} }

type finalError struct{ err error }

func (e *finalError) Error() string { return e.err.Error() }
func (e *finalError) Unwrap() error { return e.err }

// Final marks err as the last answer about a job (it failed at the vendor): asking again returns the
// same answer, so Repeat returns it at once even when Hooks.Unavailable accepts it (a 5xx).
func Final(err error) error { return &finalError{err: err} }

func retryAfterOf(err error) (after time.Duration) {
	var w *waitError
	if errors.As(err, &w) {
		after = w.after
	}
	return after
}

// Repeat runs fn until it succeeds, fails with an error that Hooks.Unavailable (and Retryable, when
// set) does not accept or that is Final, or ctx ends; in the last cases it returns that last error.
// It has no attempt limit: how long a failing request is retried is the deadline of ctx, which the
// caller sets to the lifetime of the job. It is meant for a GET of a job that was accepted (and is
// billed) at the vendor, where giving up early loses the output: a vendor outage of some seconds, or
// a 429 whose Retry-After is long, must not.
//
// The wait before a retry is the exponential backoff, or the Retry-After (Hooks.RetryAfter, else
// WithRetryAfter) when that is longer, capped at Max (or Base when that is larger). Hooks.Sleep
// defaults to SleepContext. fn must be safe to repeat.
func Repeat(ctx context.Context, bo Backoff, h Hooks, fn func() error) error {
	sleep, retryAfter := h.Sleep, h.RetryAfter
	if sleep == nil {
		sleep = SleepContext
	}
	if retryAfter == nil {
		retryAfter = retryAfterOf
	}
	limit := max(bo.Max, bo.Base)
	delay := bo.Base
	for attempt := 1; ; attempt++ {
		err := fn()
		var final *finalError
		if err == nil || ctx.Err() != nil || errors.As(err, &final) || !h.Unavailable(err) || (h.Retryable != nil && !h.Retryable(err)) {
			return err
		}
		wait := min(max(delay, retryAfter(err)), limit)
		if h.OnRetry != nil {
			h.OnRetry(attempt, wait, err)
		}
		if sleep(ctx, wait) != nil {
			return err
		}
		delay = min(delay*2, limit)
	}
}
