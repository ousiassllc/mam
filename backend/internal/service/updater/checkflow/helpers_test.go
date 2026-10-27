// Package checkflow_test holds the Checker download-hardening and Status
// classification tests. They only use updater's exported API and live in
// their own directory to keep backend/internal/service/updater under the linterly
// line budget. The helpers below are this package's own (sibling test
// packages do not import each other).
package checkflow_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/service/updater"
	"github.com/ousiassllc/mam/backend/internal/version"
)

// setVersion overrides backend/internal/version.Version for the duration of the
// test (t.Cleanup restores it), since Checker.CheckForUpdate compares the
// latest release's tag_name against this build-time-embedded global.
func setVersion(t *testing.T, v string) {
	t.Helper()
	original := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = original })
}

// fakeGate is a Gate that refuses with block (empty: allows) and counts its calls.
type fakeGate struct {
	block updater.BlockKind
	calls *int
}

func (g fakeGate) SafeToUpdate(context.Context) (bool, updater.BlockReason) {
	if g.calls != nil {
		*g.calls++
	}
	if g.block != "" {
		return false, updater.BlockReason{Kind: g.block, Detail: "fake"}
	}
	return true, updater.BlockReason{}
}

// allowGate is a Gate every call to SafeToUpdate reports safe for: tests that only care about the
// semver / download / verification pipeline use it.
func allowGate() updater.Gate { return fakeGate{} }

// blockGate is a Gate that always refuses with kind.
func blockGate(kind updater.BlockKind) updater.Gate { return fakeGate{block: kind} }

const installerBody = "fake-installer-bytes-for-update-tests"

func installerChecksum(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(installerBody))
	return hex.EncodeToString(sum[:])
}

// newGitHubMock serves `/repos/{owner}/{repo}/releases/latest` plus the
// installer/checksums asset download URLs it references.
func newGitHubMock(t *testing.T, tagName string, checksumsBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	var serverURL string
	mux.HandleFunc("/repos/ousiassllc/mam/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		release := updater.Release{
			TagName: tagName,
			Assets: []updater.Asset{
				{Name: installerName, BrowserDownloadURL: serverURL + "/download/installer.exe"},
				{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(release)
	})
	mux.HandleFunc("/download/installer.exe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(installerBody))
	})
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksumsBody))
	})

	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

func newChecker(server *httptest.Server, gate updater.Gate) *updater.Checker {
	return updater.NewChecker(updater.Config{
		Owner:      "ousiassllc",
		Repo:       "mam",
		Gate:       gate,
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
		// newGitHubMock serves assets under /download/ on the same server.
		DownloadURLPrefix: server.URL + "/download/",
		// The fixtures carry the Windows installer: the same on every host OS (the zero value follows runtime.GOOS).
		Platform: updater.PlatformWindowsAMD64,
	})
}
