package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Retry bounds the automatic resume of a download whose connection broke. The zero value is the production setting:
// six transfers in all, the waits between them 2 s, 4 s, 8 s, 16 s and 30 s.
type Retry struct {
	// Attempts is the number of transfers in total, the first one included (default 6; 1 never resumes).
	Attempts int
	// Backoff is the wait before the first resume; each next wait is twice as long (default 2 s).
	Backoff time.Duration
	// MaxBackoff bounds one wait (default 30 s).
	MaxBackoff time.Duration
}

const (
	defaultAttempts   = 6
	defaultBackoff    = 2 * time.Second
	defaultMaxBackoff = 30 * time.Second
)

func (r Retry) attempts() int {
	if r.Attempts > 0 {
		return r.Attempts
	}
	return defaultAttempts
}

// wait is the pause after the failed transfer number n (1 is the first transfer).
func (r Retry) wait(n int) time.Duration {
	d, limit := cmpDuration(r.Backoff, defaultBackoff), cmpDuration(r.MaxBackoff, defaultMaxBackoff)
	for i := 1; i < n && d < limit; i++ {
		d *= 2
	}
	return min(d, limit)
}

// RetryEvent is what OnRetry is told when a download is about to be resumed.
type RetryEvent struct {
	// Dest is the file that is downloaded.
	Dest string
	// Attempt is the number of the transfer that failed (1 is the first); Max is how many transfers are made at most.
	Attempt, Max int
	// Wait is the pause before the next transfer, which continues from the part file.
	Wait time.Duration
	// Err is why the transfer failed.
	Err error
}

// ErrStalled is why a transfer was cut: no byte arrived within Options.IdleTimeout.
var ErrStalled = errors.New("fetch: no data arrived within the idle timeout")

// ConnectionLostError is the failure of a download whose connection broke (reset, cut off, timed out, stalled) on every
// transfer that Options.Retry allows. The part file stays: running the download again resumes from it.
type ConnectionLostError struct {
	// Attempts is the number of transfers that were made.
	Attempts int
	Err      error
}

func (e *ConnectionLostError) Error() string {
	return fmt.Sprintf("fetch: the connection was lost (%d transfers): %v", e.Attempts, e.Err)
}

func (e *ConnectionLostError) Unwrap() error { return e.Err }

// Transient reports whether the same request may answer differently later: a server error, a request timeout or a rate limit.
func (e *HTTPError) Transient() bool {
	return e.Status >= http.StatusInternalServerError || e.Status == http.StatusRequestTimeout || e.Status == http.StatusTooManyRequests
}

// bodyError is a failure of the connection while the body was read: the answer had started, so the network is the cause.
type bodyError struct{ err error }

func (e *bodyError) Error() string { return e.err.Error() }
func (e *bodyError) Unwrap() error { return e.err }

// connectionLost reports whether err is the network breaking (a part file that was written so far can be continued), as
// opposed to a refusal (the address guard, a bad answer), a failure of the local disk or a verdict on the content.
func connectionLost(err error) bool {
	var (
		body *bodyError
		dns  *net.DNSError
		nerr net.Error
	)
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrStalled), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF), errors.As(err, &body), isReset(err):
		return true
	case errors.As(err, &dns):
		return dns.IsTimeout || dns.IsTemporary
	case errors.As(err, &nerr):
		return nerr.Timeout()
	}
	return false
}

// transferResuming makes transfers until one completes or fails for good: a lost connection (or a server that answers a
// server error, a request timeout or a rate limit) is resumed from the part file after a wait, up to Options.Retry. A
// cancelled ctx, a verdict on the content, a failure of the disk and any other answer end it at once.
func transferResuming(ctx context.Context, dest, part string, have int64, src Source, o Options) error {
	limit := o.Retry.attempts()
	for attempt := 1; ; attempt++ {
		err := transfer(ctx, part, have, src, o)
		var he *HTTPError
		lost := connectionLost(err)
		transient := errors.As(err, &he) && he.Transient()
		if err == nil || ctx.Err() != nil || !lost && !transient {
			return err
		}
		if attempt >= limit {
			if lost {
				return &ConnectionLostError{Attempts: attempt, Err: err}
			}
			return err
		}
		wait := o.Retry.wait(attempt)
		if o.OnRetry != nil {
			o.OnRetry(ctx, RetryEvent{Dest: dest, Attempt: attempt, Max: limit, Wait: wait, Err: err})
		}
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return context.Cause(ctx)
		}
		var perr error
		if have, perr = partSize(part, src.Size); perr != nil {
			return perr
		}
	}
}
