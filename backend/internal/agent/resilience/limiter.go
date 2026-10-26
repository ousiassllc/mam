package resilience

import (
	"context"
	"sync"
)

// Limiter bounds the calls in flight to one vendor. The limit starts at max, is halved when the
// vendor answers 429 (never below 1) and grows by one after as many successful calls as the
// current limit (additive increase, multiplicative decrease), so the concurrency follows the rate
// limit of the account instead of a guess.
type Limiter struct {
	mu       sync.Mutex
	max      int
	limit    int
	inflight int
	streak   int
	waiters  []chan struct{}
}

// NewLimiter returns a Limiter whose limit starts at max.
func NewLimiter(max int) *Limiter { return &Limiter{max: max, limit: max} }

// Max is the limit the Limiter started with and grows back to.
func (l *Limiter) Max() int { return l.max }

// Acquire waits for a free slot. The returned function gives the slot back; ok tells whether the
// call succeeded (it feeds the growth of the limit).
func (l *Limiter) Acquire(ctx context.Context) (release func(ok bool), err error) {
	for {
		l.mu.Lock()
		if l.inflight < l.limit {
			l.inflight++
			l.mu.Unlock()
			var once sync.Once
			return func(ok bool) { once.Do(func() { l.release(ok) }) }, nil
		}
		ch := make(chan struct{})
		l.waiters = append(l.waiters, ch)
		l.mu.Unlock()

		select {
		case <-ch:
		case <-ctx.Done():
			l.mu.Lock()
			removed := l.remove(ch)
			if !removed { // already woken: hand the wake-up to the next waiter
				l.wake()
			}
			l.mu.Unlock()
			return nil, ctx.Err()
		}
	}
}

func (l *Limiter) remove(ch chan struct{}) bool {
	for i, w := range l.waiters {
		if w == ch {
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			return true
		}
	}
	return false
}

// wake lets waiters retry while there is capacity. The caller holds l.mu.
func (l *Limiter) wake() {
	for free := l.limit - l.inflight; free > 0 && len(l.waiters) > 0; free-- {
		close(l.waiters[0])
		l.waiters = l.waiters[1:]
	}
}

// Throttle lowers the limit after a 429.
func (l *Limiter) Throttle() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = max(1, l.limit/2)
	l.streak = 0
}

func (l *Limiter) release(ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inflight--
	if ok {
		l.streak++
		if l.limit < l.max && l.streak >= l.limit {
			l.limit++
			l.streak = 0
		}
	}
	l.wake()
}

// Current returns the present limit (tests and diagnostics).
func (l *Limiter) Current() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}
