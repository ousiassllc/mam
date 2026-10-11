// Package testenv holds the helpers tests use to react to a missing external tool.
package testenv

import (
	"os"
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
