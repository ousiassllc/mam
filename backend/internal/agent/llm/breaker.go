package llm

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

// breaker is the circuit breaker of one provider. Closed: calls pass. After threshold
// consecutive failures it opens and rejects calls until the open period is over. Then it
// lets exactly one probe call through (half-open): a success closes it, a failure opens it again.
type breaker struct {
	mu        sync.Mutex
	now       func() time.Time
	threshold int
	openFor   time.Duration

	state    breakerState
	failures int
	openedAt time.Time
}

func newBreaker(threshold int, openFor time.Duration, now func() time.Time) *breaker {
	return &breaker{now: now, threshold: threshold, openFor: openFor}
}

// allow reports whether a call may be made now.
func (b *breaker) allow() bool {
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

// isOpen reports whether the breaker currently rejects calls, without consuming the probe.
func (b *breaker) isOpen() bool {
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

func (b *breaker) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state, b.failures = breakerClosed, 0
}

// failure records a failed call that says the provider is unavailable.
func (b *breaker) failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.state == breakerHalfOpen || b.failures >= b.threshold {
		b.state, b.openedAt = breakerOpen, b.now()
	}
}

// release ends a probe that produced no verdict (the caller gave up, or the error says
// nothing about availability): the breaker goes back to the state it had before the probe.
func (b *breaker) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == breakerHalfOpen {
		b.state = breakerOpen
	}
}
