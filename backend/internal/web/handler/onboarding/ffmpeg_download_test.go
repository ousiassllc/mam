package onboarding_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func fakeDownload(t *testing.T, goos, goarch string) *ffmpegdl.Service {
	t.Helper()
	p := ffmpegdl.Platform{
		Version: "9.9.9", Build: "test build", Publisher: "Test Publisher", SourceCodeURL: "https://example.com/src",
		License: ffmpegdl.License{ID: "GPL-3.0-or-later", Name: "GPL v3", URL: "https://example.com/gpl"},
		Archives: []ffmpegdl.Archive{{URL: "https://example.com/a.zip", Format: "zip", SHA256: "x", Size: 1 << 20,
			Members: []ffmpegdl.Member{{Entry: "a", Name: "ffmpeg", Size: 1 << 20}, {Entry: "b", Name: "ffprobe", Size: 1 << 20}}}},
	}
	s := ffmpegdl.New(ffmpegdl.Options{
		Layout: ffmpegdl.Layout{Root: filepath.Join(t.TempDir(), "ffmpeg")}, GOOS: goos, GOARCH: goarch,
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): p}},
	})
	t.Cleanup(s.Close)
	return s
}

func missing(string) (string, error) { return "", assert.AnError }

// Issue #900: where ffmpeg can be downloaded, the step has the button (the same panel and routes as the settings screen), above
// the way to install it yourself, which stays.
func TestFFmpegStep_OffersTheDownloadWhereItIsAvailableAndSomethingIsMissing(t *testing.T) {
	for name, tc := range map[string]struct {
		goos     string
		look     func(string) (string, error)
		download *ffmpegdl.Service
		want     []string
		not      []string
	}{
		"missing, supported": {
			"linux", missing, fakeDownload(t, "linux", "amd64"),
			[]string{`data-testid="ffmpeg-download"`, `data-testid="ffmpeg-download-button"`, "hx-post=\"/settings/ffmpeg/download\"", "SHA-256", "onboarding-ffmpeg-other", "onboarding-ffmpeg-manual"}, nil,
		},
		"missing on Windows keeps winget as the manual way": {
			"windows", missing, fakeDownload(t, "linux", "amd64"),
			[]string{`data-testid="ffmpeg-download-button"`, "winget install Gyan.FFmpeg"}, nil,
		},
		"found: nothing to download": {
			"linux", foundBoth, fakeDownload(t, "linux", "amd64"),
			[]string{`data-testid="onboarding-ffmpeg-found"`}, []string{`data-testid="ffmpeg-download"`},
		},
		"missing, platform with no build": {
			"linux", missing, fakeDownload(t, "linux", "arm64"),
			[]string{`data-testid="onboarding-ffmpeg-other"`}, []string{`data-testid="ffmpeg-download"`, "onboarding-ffmpeg-manual"},
		},
		"missing, the app has no download": {
			"linux", missing, nil,
			[]string{`data-testid="onboarding-ffmpeg-other"`}, []string{`data-testid="ffmpeg-download"`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, func(c *config) { c.goos, c.look, c.download = tc.goos, tc.look, tc.download })
			body := r.page(paths.OnboardingFFmpeg)
			for _, w := range tc.want {
				assert.Contains(t, body, w)
			}
			for _, n := range tc.not {
				assert.NotContains(t, body, n)
			}
		})
	}
}
