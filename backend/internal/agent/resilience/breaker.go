// Package resilience holds the fault tolerance shared by the Reasoning LLM Adapter and the
// Model Adapter (spec/requirements/non-functional.md §6, spec/architecture/model-adapters.md §7):
// a per-provider circuit breaker and a retry loop with exponential backoff.
package resilience

import (
	"sync"
	"time"
)

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// Breaker is the circuit breaker of one provider. Closed: calls pass. After threshold
// consecutive failures it opens and rejects calls until the open period is over. Then it
// lets exactly one probe call through (half-open): a success closes it, a failure opens it again.
type Breaker struct {
	mu        sync.Mutex
	now       func() time.Time
	threshold int
	openFor   time.Duration

	state    breakerState
	failures int
	openedAt time.Time
}

// NewBreaker returns a closed Breaker that opens after threshold consecutive failures and
// stays open for openFor. now is the time source.
func NewBreaker(threshold int, openFor time.Duration, now func() time.Time) *Breaker {
	return &Breaker{now: now, threshold: threshold, openFor: openFor}
}

// Allow reports whether a call may be made now.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		if b.now().Sub(b.openedAt) < b.openFor {
			return false
		}
		b.state = breakerHalfOpen // this caller is the probe
		return true
	case breakerHalfOpen:
		return false // the probe is still in flight
	}
	return true
}

// IsOpen reports whether the breaker currently rejects calls, without consuming the probe.
func (b *Breaker) IsOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		return b.now().Sub(b.openedAt) < b.openFor
	case breakerHalfOpen:
		return true
	}
	return false
}

// Success records a successful call: the breaker closes.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state, b.failures = breakerClosed, 0
}

// Failure records a failed call that says the provider is unavailable.
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.state == breakerHalfOpen || b.failures >= b.threshold {
		b.state, b.openedAt = breakerOpen, b.now()
	}
}

// Release ends a probe that produced no verdict (the caller gave up, or the error says
// nothing about availability): the breaker goes back to the state it had before the probe.
func (b *Breaker) Release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		b.state = breakerOpen
	}
}
