package fetch_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// step is what the flaky server does with one request: it sends after bytes of the answer (all of them when after is 0), then
// ends the connection as kind says.
type step struct {
	kind  string // "ok", "cut" (closed in order: the body is short), "reset" (a TCP reset), "stall" (silence), or a status code
	after int
}

// flaky serves payload with Range support; its n-th request does what plan[n] says, the ones after the plan are served in full.
type flaky struct {
	*httptest.Server
	mu     sync.Mutex
	plan   []step
	ranges []string
}

func newFlaky(t *testing.T, plan ...step) *flaky {
	t.Helper()
	f := &flaky{plan: plan}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *flaky) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	n := len(f.ranges)
	f.ranges = append(f.ranges, r.Header.Get("Range"))
	st := step{kind: "ok"}
	if n < len(f.plan) {
		st = f.plan[n]
	}
	f.mu.Unlock()
	if code, err := strconv.Atoi(st.kind); err == nil {
		w.WriteHeader(code)
		return
	}
	start := 0
	if rg := r.Header.Get("Range"); rg != "" {
		start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(payload)-1)+"/"+strconv.Itoa(len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)-start))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	}
	out := payload[start:]
	if st.kind == "ok" || st.after >= len(out) {
		_, _ = w.Write(out)
		return
	}
	if st.after > 0 {
		_, _ = w.Write(out[:st.after])
		w.(http.Flusher).Flush()
	}
	switch st.kind {
	case "cut":
		panic(http.ErrAbortHandler) // closes the connection with the body unfinished
	case "reset":
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_ = conn.(*net.TCPConn).SetLinger(0) // a close now sends a reset instead of a FIN
		_ = conn.Close()
	case "stall":
		<-r.Context().Done()
	}
}

func (f *flaky) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ranges...)
}

// fast is a download that resumes up to attempts transfers in all, with waits that a test does not notice.
func fast(attempts int, extra ...func(*fetch.Options)) fetch.Options {
	return opts(append([]func(*fetch.Options){func(o *fetch.Options) {
		o.Retry = fetch.Retry{Attempts: attempts, Backoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond}
	}}, extra...)...)
}

func destIn(t *testing.T) string { return filepath.Join(t.TempDir(), "m.safetensors") }

func requireComplete(t *testing.T, dest string) {
	t.Helper()
	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.NoFileExists(t, fetch.PartPath(dest))
}

// A connection that breaks again and again (the server closes it, resets it, answers nothing before the body) is resumed from
// the part file each time and the download completes, with the SHA-256 of the whole file.
func TestDownloadResumesByItselfAfterTheConnectionBreaks(t *testing.T) {
	srv := newFlaky(t, step{"cut", 300_000}, step{"reset", 100_000}, step{"reset", 0}, step{"cut", 50_000})
	dest := destIn(t)
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), fast(6)))
	requireComplete(t, dest)
	reqs := srv.requests()
	require.Len(t, reqs, 5)
	assert.Empty(t, reqs[0])
	assert.Equal(t, "bytes=300000-", reqs[1], "the first cut left 300000 bytes: the next request asks for the rest")
}

// The progress goes on from the bytes in place after a resume, and every wait is told to OnRetry: doubling up to the cap.
func TestDownloadWaitsLongerBetweenResumesUpToACap(t *testing.T) {
	srv := newFlaky(t, step{"cut", 1000}, step{"cut", 1000}, step{"cut", 1000}, step{"cut", 1000})
	dest := destIn(t)
	var events []fetch.RetryEvent
	var first []int64
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), fast(5, func(o *fetch.Options) {
		o.OnRetry = func(_ context.Context, e fetch.RetryEvent) { events = append(events, e) }
		o.Progress = func(done, _ int64) { first = append(first, done) }
	})))
	requireComplete(t, dest)
	require.Len(t, events, 4)
	var waits []time.Duration
	for i, e := range events {
		waits = append(waits, e.Wait)
		assert.Equal(t, i+1, e.Attempt)
		assert.Equal(t, 5, e.Max)
		assert.Equal(t, dest, e.Dest)
		assert.ErrorIs(t, e.Err, io.ErrUnexpectedEOF)
	}
	assert.Equal(t, []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 4 * time.Millisecond}, waits)
	assert.Equal(t, int64(len(payload)), first[len(first)-1])
	assert.Less(t, first[1], int64(len(payload)), "the progress did not start over")
}

// When every transfer breaks, the download stops after the last one with a ConnectionLostError and keeps the part file, which the
// next run continues: the bytes of the transfers add up.
func TestDownloadGivesUpAfterTheLastAttemptAndKeepsThePartFile(t *testing.T) {
	srv := newFlaky(t, step{"cut", 300_000}, step{"cut", 300_000}, step{"cut", 300_000})
	dest := destIn(t)
	err := fetch.Download(context.Background(), dest, source(srv.URL), fast(3))
	var lost *fetch.ConnectionLostError
	require.ErrorAs(t, err, &lost)
	assert.Equal(t, 3, lost.Attempts)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Equal(t, []string{"", "bytes=300000-", "bytes=600000-"}, srv.requests(), "no transfer after the last")
	st, err := os.Stat(fetch.PartPath(dest))
	require.NoError(t, err)
	assert.Equal(t, int64(900_000), st.Size())
	assert.NoFileExists(t, dest)

	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), fast(3)))
	requireComplete(t, dest)
	assert.Equal(t, "bytes=900000-", srv.requests()[3], "the next run continues from the part file")
}

// A transfer that delivers nothing for the idle timeout is cut and resumed like a broken connection.
func TestDownloadResumesAStalledTransfer(t *testing.T) {
	srv := newFlaky(t, step{"stall", 1000})
	dest := destIn(t)
	var events []fetch.RetryEvent
	require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), fast(3, func(o *fetch.Options) {
		o.IdleTimeout = 300 * time.Millisecond
		o.OnRetry = func(_ context.Context, e fetch.RetryEvent) { events = append(events, e) }
	})))
	requireComplete(t, dest)
	require.Len(t, events, 1)
	assert.ErrorIs(t, events[0].Err, fetch.ErrStalled)
	assert.Len(t, srv.requests(), 2)
}

// A stalled transfer that is not resumed ends as a lost connection too.
func TestDownloadReportsAStalledTransferAsALostConnection(t *testing.T) {
	srv := newFlaky(t, step{"stall", 1000})
	err := fetch.Download(context.Background(), destIn(t), source(srv.URL), opts(func(o *fetch.Options) { o.IdleTimeout = 300 * time.Millisecond }))
	var lost *fetch.ConnectionLostError
	require.ErrorAs(t, err, &lost)
	assert.Equal(t, 1, lost.Attempts)
	assert.ErrorIs(t, err, fetch.ErrStalled)
}

// A server error, a request timeout and a rate limit are waited out; the failure after the last attempt is the answer itself.
func TestDownloadResumesAfterATransientStatus(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusRequestTimeout, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv := newFlaky(t, step{kind: strconv.Itoa(status)}, step{kind: strconv.Itoa(status)})
			dest := destIn(t)
			require.NoError(t, fetch.Download(context.Background(), dest, source(srv.URL), fast(3)))
			requireComplete(t, dest)
			assert.Len(t, srv.requests(), 3)
		})
	}
	srv := newFlaky(t, step{kind: "503"}, step{kind: "503"}, step{kind: "503"})
	err := fetch.Download(context.Background(), destIn(t), source(srv.URL), fast(3))
	var he *fetch.HTTPError
	require.ErrorAs(t, err, &he)
	assert.Equal(t, http.StatusServiceUnavailable, he.Status)
	assert.NotErrorAs(t, err, new(*fetch.ConnectionLostError), "the server answered: the connection was not lost")
	assert.Len(t, srv.requests(), 3)
}

// What no new try can change is not tried again: a refusal of the server, a file that is not the pinned one, the user's cancel.
func TestDownloadDoesNotRetryWhatCannotGetBetter(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		srv := newFlaky(t, step{kind: "404"})
		err := fetch.Download(context.Background(), destIn(t), source(srv.URL), fast(5, failOnRetry(t)))
		var he *fetch.HTTPError
		require.ErrorAs(t, err, &he)
		assert.Len(t, srv.requests(), 1)
	})
	t.Run("checksum", func(t *testing.T) {
		bad := append([]byte{}, payload...)
		bad[10] ^= 0xff
		srv := newServer(t, bad)
		err := fetch.Download(context.Background(), destIn(t), source(srv.URL), fast(5, failOnRetry(t)))
		require.ErrorIs(t, err, fetch.ErrChecksum)
		assert.Len(t, srv.requests(), 1)
	})
	t.Run("cancel", func(t *testing.T) {
		srv := newFlaky(t, step{"stall", 100_000})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		dest := destIn(t)
		err := fetch.Download(ctx, dest, source(srv.URL), fast(5, failOnRetry(t), func(o *fetch.Options) {
			o.Progress = func(int64, int64) { cancel() } // the user stops the download once bytes arrive
		}))
		require.ErrorIs(t, err, context.Canceled)
		assert.NotErrorAs(t, err, new(*fetch.ConnectionLostError))
		assert.Len(t, srv.requests(), 1)
		assert.FileExists(t, fetch.PartPath(dest), "the part file stays for the next run")
	})
}

func failOnRetry(t *testing.T) func(*fetch.Options) {
	return func(o *fetch.Options) {
		o.OnRetry = func(_ context.Context, e fetch.RetryEvent) { t.Errorf("retried after %v", e.Err) }
	}
}

// The wait for the next transfer ends with the cancel: the user does not wait for a backoff to stop an install.
func TestDownloadStopsWaitingToResumeWhenCancelled(t *testing.T) {
	srv := newFlaky(t, step{"cut", 1000})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan struct{})
	errc := make(chan error, 1)
	go func() {
		errc <- fetch.Download(ctx, destIn(t), source(srv.URL), opts(func(o *fetch.Options) {
			o.Retry = fetch.Retry{Attempts: 3, Backoff: time.Hour, MaxBackoff: time.Hour}
			o.OnRetry = func(context.Context, fetch.RetryEvent) { close(waiting) }
		}))
	}()
	<-waiting
	cancel()
	select {
	case err := <-errc:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("the download kept waiting for its backoff")
	}
	assert.Len(t, srv.requests(), 1)
}

// A refused address is not a lost connection: the guard's refusal ends the download at once.
func TestDownloadDoesNotRetryARefusedAddress(t *testing.T) {
	refuse := errors.New("refusing to connect to the non-public address 10.0.0.1")
	var calls int
	err := fetch.Download(context.Background(), destIn(t), source("http://example.invalid/m"), fast(5, failOnRetry(t), func(o *fetch.Options) {
		o.Client = doerFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, &net.OpError{Op: "dial", Err: refuse}
		})
	}))
	require.ErrorIs(t, err, refuse)
	assert.Equal(t, 1, calls)
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
