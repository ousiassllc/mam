// Package testenv holds the helpers tests use to react to their environment: a missing external
// tool, whether a fixture may run its test in parallel, or how late a request arrives on a busy
// machine.
package testenv

import (
	"context"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// EndsAfterFirstRequest returns a context that ends grace after requests first counts a request,
// not grace after the test started: on a busy machine the first request may arrive late, and a
// fixed deadline then ends the call before any request was made. The 30 second deadline only
// guards against a request that never comes.
func EndsAfterFirstRequest(t testing.TB, requests *atomic.Int32, grace time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	go func() {
		for requests.Load() == 0 && ctx.Err() == nil {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(grace)
		cancel()
	}()
	return ctx
}

// CountGets wraps h so that every GET request it serves is counted in n. For a job API the first GET
// is the first status request: the job was accepted by then, which a POST arriving does not tell.
func CountGets(n *atomic.Int32, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			n.Add(1)
		}
		h(w, r)
	}
}

// MissingFFmpeg ends a test that needs ffmpeg / ffprobe which are not on PATH. On a developer
// machine the test is skipped; in CI (the CI environment variable is "true", as GitHub Actions
// sets it) it fails, so a runner without ffmpeg cannot turn the ffmpeg tests into a green skip.
func MissingFFmpeg(t testing.TB, msg string) {
	t.Helper()
	if os.Getenv("CI") == "true" {
		t.Fatalf("%s (CI must install ffmpeg / ffprobe: .github/workflows/ci.yml)", msg)
	}
	t.Skip(msg)
}

var parallelized sync.Map

// slots bounds how many Parallel tests run at once. Their fixtures run background workers and wait
// on them with timeouts, so on a busy machine (go test ./... runs many packages at once) one slice
// per CPU starves the workers and turns the waits into flakes.
var slots = make(chan struct{}, 8)

// Parallel is t.Parallel for fixtures: a test that builds several fixtures calls it several times,
// and t.Parallel panics when called twice on the same test.
func Parallel(t *testing.T) {
	t.Helper()
	if _, already := parallelized.LoadOrStore(t, struct{}{}); already {
		return
	}
	t.Cleanup(func() { parallelized.Delete(t) })
	t.Parallel()
	slots <- struct{}{}
	t.Cleanup(func() { <-slots })
}
