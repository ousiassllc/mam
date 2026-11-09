package fetch_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

const (
	hfURL     = "https://huggingface.co/"
	githubURL = "https://github.com/"
	within    = 10 * time.Second       // an event that must happen
	quiet     = 100 * time.Millisecond // an event that must not happen (a wrong pass is possible, a wrong failure is not)
)

// gated answers every request with the payload, but only after the test lets it through (gate) and tells the test when
// a request has reached it (started, with the host). It counts the requests in flight per host.
type gated struct {
	started chan string
	gate    chan struct{}
	mu      sync.Mutex
	now     map[string]int
	most    map[string]int
	status  map[string]int // by path: a status other than 200
}

func newGated() *gated {
	return &gated{started: make(chan string, 16), gate: make(chan struct{}), now: map[string]int{}, most: map[string]int{}, status: map[string]int{}}
}

func (g *gated) Do(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if code := g.status[r.URL.Path]; code != 0 {
		return &http.Response{StatusCode: code, Body: http.NoBody, Request: r}, nil
	}
	g.mu.Lock()
	g.now[host]++
	g.most[host] = max(g.most[host], g.now[host])
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.now[host]--
		g.mu.Unlock()
	}()
	g.started <- host
	select {
	case <-g.gate:
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(payload)), Request: r}, nil
}

func (g *gated) mostAt(host string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.most[host]
}

// run starts a download of base+path in the background and returns what it ends with.
func (g *gated) run(ctx context.Context, t *testing.T, l *fetch.HostLimiter, base, path string, extra ...func(*fetch.Options)) <-chan error {
	t.Helper()
	errc := make(chan error, 1)
	dest := filepath.Join(t.TempDir(), "m.safetensors")
	o := opts(append([]func(*fetch.Options){func(o *fetch.Options) { o.Client = g; o.Limiter = l }}, extra...)...)
	go func() { errc <- fetch.Download(ctx, dest, source(base+path), o) }()
	return errc
}

func (g *gated) awaitStart(t *testing.T, host string) {
	t.Helper()
	select {
	case got := <-g.started:
		require.Equal(t, host, got)
	case <-time.After(within):
		t.Fatalf("no request reached %s", host)
	}
}

func (g *gated) requireNoStart(t *testing.T) {
	t.Helper()
	select {
	case host := <-g.started:
		t.Fatalf("a request to %s started although no slot was free", host)
	case <-time.After(quiet):
	}
}

func awaitDone(t *testing.T, errc <-chan error) error {
	t.Helper()
	select {
	case err := <-errc:
		return err
	case <-time.After(within):
		t.Fatal("the download did not end")
		return nil
	}
}

// With a limit of 2 the third download to a host waits until one of the first two ends; no more than 2 are ever in flight.
func TestDownloadWaitsForASlotOfItsHost(t *testing.T) {
	g := newGated()
	l := fetch.NewHostLimiter(2)
	ctx := context.Background()
	done := []<-chan error{g.run(ctx, t, l, hfURL, "a"), g.run(ctx, t, l, hfURL, "b")}
	g.awaitStart(t, "huggingface.co")
	g.awaitStart(t, "huggingface.co")
	done = append(done, g.run(ctx, t, l, hfURL, "c"))
	g.requireNoStart(t)

	g.gate <- struct{}{} // one of the first two ends: the third takes its slot
	g.awaitStart(t, "huggingface.co")
	g.gate <- struct{}{}
	g.gate <- struct{}{}
	for _, errc := range done {
		require.NoError(t, awaitDone(t, errc))
	}
	assert.Equal(t, 2, g.mostAt("huggingface.co"))
}

// The limit is per host: a download to another host does not wait for the full one.
func TestDownloadOfAnotherHostDoesNotWait(t *testing.T) {
	g := newGated()
	l := fetch.NewHostLimiter(2)
	ctx := context.Background()
	done := []<-chan error{g.run(ctx, t, l, hfURL, "a"), g.run(ctx, t, l, hfURL, "b")}
	g.awaitStart(t, "huggingface.co")
	g.awaitStart(t, "huggingface.co")
	done = append(done, g.run(ctx, t, l, githubURL, "c"))
	g.awaitStart(t, "github.com")
	for range done {
		g.gate <- struct{}{}
	}
	for _, errc := range done {
		require.NoError(t, awaitDone(t, errc))
	}
}

// A download that waits for a slot ends with the cancel at once, and it gives back nothing it did not take: the slot of the
// download that runs stays taken, so the next one still waits.
func TestDownloadStopsWaitingForASlotWhenCancelled(t *testing.T) {
	g := newGated()
	l := fetch.NewHostLimiter(1)
	holder := g.run(context.Background(), t, l, hfURL, "a")
	g.awaitStart(t, "huggingface.co")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiter := g.run(ctx, t, l, hfURL, "b")
	g.requireNoStart(t)
	cancel()
	require.ErrorIs(t, awaitDone(t, waiter), context.Canceled)

	next := g.run(context.Background(), t, l, hfURL, "c")
	g.requireNoStart(t) // the cancelled download did not free the holder's slot
	g.gate <- struct{}{}
	require.NoError(t, awaitDone(t, holder))
	g.awaitStart(t, "huggingface.co")
	g.gate <- struct{}{}
	require.NoError(t, awaitDone(t, next))
	assert.Equal(t, 1, g.mostAt("huggingface.co"))
}

func TestAcquireWithACancelledContextTakesNoSlot(t *testing.T) {
	l := fetch.NewHostLimiter(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release, err := l.Acquire(ctx, "huggingface.co")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, release)
	got, err := l.Acquire(context.Background(), "HuggingFace.co") // the slot is free; the host is matched without case
	require.NoError(t, err)
	got()
	got() // giving back twice frees one slot only
	first, err := l.Acquire(context.Background(), "huggingface.co")
	require.NoError(t, err)
	defer first()
	short, stop := context.WithTimeout(context.Background(), quiet)
	defer stop()
	_, err = l.Acquire(short, "huggingface.co")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// A download that fails gives its slot back.
func TestDownloadReleasesItsSlotAfterAnError(t *testing.T) {
	g := newGated()
	g.status["/bad"] = http.StatusNotFound
	l := fetch.NewHostLimiter(1)
	var he *fetch.HTTPError
	require.ErrorAs(t, awaitDone(t, g.run(context.Background(), t, l, hfURL, "bad")), &he)
	next := g.run(context.Background(), t, l, hfURL, "ok")
	g.awaitStart(t, "huggingface.co")
	g.gate <- struct{}{}
	require.NoError(t, awaitDone(t, next))
}

// A download that waits for its backoff before it resumes holds no slot: another download to the host runs meanwhile.
func TestDownloadGivesItsSlotBackWhileItWaitsToResume(t *testing.T) {
	g := newGated()
	g.status["/flaky"] = http.StatusServiceUnavailable
	l := fetch.NewHostLimiter(1)
	waiting := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	flaky := g.run(ctx, t, l, hfURL, "flaky", func(o *fetch.Options) {
		o.Retry = fetch.Retry{Attempts: 2, Backoff: time.Hour, MaxBackoff: time.Hour}
		o.OnRetry = func(context.Context, fetch.RetryEvent) { close(waiting) }
	})
	select {
	case <-waiting:
	case <-time.After(within):
		t.Fatal("the download never waited to resume")
	}
	other := g.run(context.Background(), t, l, hfURL, "ok")
	g.awaitStart(t, "huggingface.co")
	g.gate <- struct{}{}
	require.NoError(t, awaitDone(t, other))
	cancel()
	require.ErrorIs(t, awaitDone(t, flaky), context.Canceled)
}
