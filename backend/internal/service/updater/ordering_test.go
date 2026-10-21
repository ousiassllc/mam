package updater_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// Versions are ordered as semver, not as strings: v0.10.0 is newer than v0.9.0, and a prerelease is
// older than its release.
func TestChecker_CheckForUpdate_SemverOrdering(t *testing.T) {
	tests := []struct {
		current, latest string
		wantReady       bool
	}{
		{"v0.9.0", "v0.10.0", true},
		{"v0.10.0", "v0.9.0", false},
		{"v1.0.0-rc.1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc.1", false},
		{"v0.1.1", "v0.1.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.current+"->"+tt.latest, func(t *testing.T) {
			setVersion(t, tt.current)
			checksums := fmt.Sprintf("%s  %s\n", installerChecksum(t), installerName)
			result, err := newChecker(newGitHubMock(t, tt.latest, checksums), allowGate()).CheckForUpdate(context.Background())
			if err != nil {
				t.Fatalf("CheckForUpdate: %v", err)
			}
			if result.Ready != tt.wantReady {
				t.Fatalf("Ready = %v, want %v", result.Ready, tt.wantReady)
			}
			if result.Ready {
				_ = os.RemoveAll(result.InstallerPath)
			}
		})
	}
}

// A gate that refuses stops the check before anything is downloaded.
func TestChecker_CheckForUpdate_ClosedGateDownloadsNothing(t *testing.T) {
	setVersion(t, "v0.1.0")
	downloads := 0
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/repos/ousiassllc/mam-releases/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(updater.Release{TagName: "v0.2.0", Assets: []updater.Asset{
			{Name: installerName, BrowserDownloadURL: serverURL + "/download/installer.exe"},
			{Name: "checksums.txt", BrowserDownloadURL: serverURL + "/download/checksums.txt"},
		}})
	})
	mux.HandleFunc("/download/", func(http.ResponseWriter, *http.Request) { downloads++ })
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)

	checker := newChecker(server, blockGate(updater.BlockJobs))
	result, err := checker.CheckForUpdate(context.Background())
	if err != nil || result.Ready {
		t.Fatalf("CheckForUpdate = %+v, %v; want not Ready, no error", result, err)
	}
	if downloads != 0 {
		t.Fatalf("%d asset downloads, want none while the gate is closed", downloads)
	}
	if s := checker.Status(); !s.Blocked || s.BlockedKind != updater.BlockJobs || !s.Available {
		t.Fatalf("Status = %+v, want Available and Blocked", s)
	}
}
