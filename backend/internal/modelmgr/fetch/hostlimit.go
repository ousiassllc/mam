package fetch

import (
	"context"
	"net/url"
	"strings"
	"sync"
)

// MaxPerHost is how many downloads may run against one host at the same time (Hugging Face, GitHub, ...). The slots are
// shared by every Download of the process, whichever job asks; a download above the limit waits for a slot. The total is
// bounded by the job workers (jobqueue.DefaultWorkers) on top of this.
const MaxPerHost = 2

// HostLimiter caps the concurrent transfers per host: at most n at a time to one host, none across hosts. The zero value
// is not usable; make one with NewHostLimiter.
type HostLimiter struct {
	n       int
	mu      sync.Mutex
	slots   map[string]chan struct{}
	waiting map[string]int
}

// NewHostLimiter returns a limiter that lets n transfers (at least 1) run against one host at the same time.
func NewHostLimiter(n int) *HostLimiter {
	return &HostLimiter{n: max(n, 1), slots: map[string]chan struct{}{}, waiting: map[string]int{}}
}

// sharedLimiter is the limiter of every Download that sets no Options.Limiter: the process-wide MaxPerHost cap.
var sharedLimiter = NewHostLimiter(MaxPerHost)

// hostKey is the key of a host: lower case, no port, no trailing dot.
func hostKey(host string) string { return strings.TrimSuffix(strings.ToLower(host), ".") }

func (l *HostLimiter) slot(host string) chan struct{} {
	k := hostKey(host)
	l.mu.Lock()
	defer l.mu.Unlock()
	ch, ok := l.slots[k]
	if !ok {
		ch = make(chan struct{}, l.n)
		l.slots[k] = ch
	}
	return ch
}

// Waiting is how many Acquire calls are parked for a slot of host right now (a full host; a call that found a free slot never counts).
func (l *HostLimiter) Waiting(host string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.waiting[hostKey(host)]
}

func (l *HostLimiter) wait(host string, delta int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.waiting[hostKey(host)] += delta
}

// Acquire waits for a free slot of host and returns the function that gives it back (call it exactly once). A cancelled
// ctx ends the wait at once with its cause and takes no slot, so there is nothing to give back.
func (l *HostLimiter) Acquire(ctx context.Context, host string) (release func(), err error) {
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	ch := l.slot(host)
	take := func() func() {
		var once sync.Once
		return func() { once.Do(func() { <-ch }) }
	}
	select {
	case ch <- struct{}{}:
		return take(), nil
	default:
	}
	l.wait(host, 1)
	defer l.wait(host, -1)
	select {
	case ch <- struct{}{}:
		return take(), nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func (o Options) limiter() *HostLimiter {
	if o.Limiter != nil {
		return o.Limiter
	}
	return sharedLimiter
}

// transferLimited is one transfer inside a slot of the host the download was asked from (src.URL, not the CDN that a redirect
// leads to: the catalog names one host per file, and the limit protects the one the user's downloads converge on). The slot is
// held for the transfer only: transferResuming gives it back before it waits for a backoff and asks again for the resume, so a
// download that waits to be resumed does not keep another one out. A wait for a slot is not a failure and is not reported as
// progress; the idle timeout starts with the transfer, after the slot was taken.
func transferLimited(ctx context.Context, part string, have int64, src Source, o Options) error {
	u, err := url.Parse(src.URL)
	if err != nil || u.Hostname() == "" {
		return transfer(ctx, part, have, src, o) // reports the refused URL
	}
	if err := o.check()(u); err != nil {
		return err // a URL that is refused does not wait for a slot, nor does it leave a host behind in the limiter
	}
	release, err := o.limiter().Acquire(ctx, u.Hostname())
	if err != nil {
		return err
	}
	defer release()
	return transfer(ctx, part, have, src, o)
}
