// Package testenv holds the helpers tests use to react to their environment: a missing external
// tool, or whether a fixture may run its test in parallel.
package testenv

import (
	"os"
	"sync"
	"testing"
)

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
