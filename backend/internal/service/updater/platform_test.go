package updater_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// A macOS (Apple Silicon) Checker takes the dmg of the release: it is downloaded, verified against the checksums.txt line
// of the same name and handed over as Ready; a release that only has the Windows installer is ErrorRelease.
func TestChecker_CheckForUpdate_DarwinDownloadsAndVerifiesTheDmg(t *testing.T) {
	setVersion(t, "v0.1.0")
	const dmg = "mam-0.2.0-darwin-arm64.dmg"
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ousiassllc/mam/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: []updater.Asset{
			{Name: "mam-0.2.0-windows-amd64-setup.exe", BrowserDownloadURL: serverURL + "/download/setup.exe"},
			{Name: dmg, BrowserDownloadURL: serverURL + "/download/mam.dmg"},
			{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
		}})
	})
	mux.HandleFunc("/download/mam.dmg", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(installerBody)) })
	mux.HandleFunc("/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%064d  mam-0.2.0-windows-amd64-setup.exe\n%s  %s\n", 0, installerChecksum(t), dmg)
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)

	checker := updater.NewChecker(updater.Config{
		Owner: "ousiassllc", Repo: "mam", Gate: allowGate(), HTTPClient: server.Client(), BaseURL: server.URL,
		DownloadURLPrefix: server.URL + "/download/", Platform: updater.PlatformDarwinARM64,
	})
	result, err := checker.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(result.InstallerPath)) })
	if !result.Ready || filepath.Base(result.InstallerPath) != dmg {
		t.Fatalf("result = %+v, want Ready with the %s", result, dmg)
	}
	if got, err := os.ReadFile(result.InstallerPath); err != nil || string(got) != installerBody {
		t.Fatalf("downloaded dmg = %q, %v", got, err)
	}

	// The same release for a Windows-only release set: no dmg.
	windowsOnly := newGitHubMock(t, "v0.2.0", fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName))
	mac := updater.NewChecker(updater.Config{
		Owner: "ousiassllc", Repo: "mam", Gate: allowGate(), HTTPClient: windowsOnly.Client(), BaseURL: windowsOnly.URL,
		DownloadURLPrefix: windowsOnly.URL + "/download/", Platform: updater.PlatformDarwinARM64,
	})
	if _, err := mac.CheckForUpdate(context.Background()); err == nil {
		t.Fatal("a release without the dmg: err = nil, want ErrorRelease")
	}
	if st := mac.Status(); st.ErrorKind != updater.ErrorRelease {
		t.Fatalf("ErrorKind = %q, want %q", st.ErrorKind, updater.ErrorRelease)
	}
}
