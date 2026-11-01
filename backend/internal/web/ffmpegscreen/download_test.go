package ffmpegscreen_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const (
	dlFFmpeg  = "#!/bin/sh\necho 'ffmpeg version 9.9.9'\n"
	dlFFprobe = "#!/bin/sh\necho 'ffprobe version 9.9.9'\n"
)

// downloadRig is a download service over a test server that serves a tiny fake build; the checksums are right unless a test breaks one.
type downloadRig struct {
	svc  *ffmpegdl.Service
	gate chan struct{} // closed: the server delivers; nil: at once
	mu   sync.Mutex
}

func newDownloadRig(t *testing.T, sha string, hold bool) *downloadRig {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range [][2]string{{"build/bin/ffmpeg", dlFFmpeg}, {"build/bin/ffprobe", dlFFprobe}} {
		w, err := zw.Create(f[0])
		require.NoError(t, err)
		_, _ = w.Write([]byte(f[1]))
	}
	require.NoError(t, zw.Close())
	body := buf.Bytes()
	r := &downloadRig{}
	if hold {
		r.gate = make(chan struct{})
		t.Cleanup(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			select {
			case <-r.gate:
			default:
				close(r.gate)
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.gate != nil {
			select {
			case <-r.gate:
			case <-req.Context().Done():
				return
			}
		}
		http.ServeContent(w, req, "ffmpeg.zip", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	h := func(s string) string { return s }
	if sha == "" {
		sha = hexSum(body)
	}
	platform := ffmpegdl.Platform{
		Version: "9.9.9", Build: "test build", Publisher: "Test Publisher",
		SourceCodeURL: "https://example.com/source", License: ffmpegdl.License{ID: "GPL-3.0-or-later", Name: "GPL v3", URL: "https://example.com/gpl"},
		Archives: []ffmpegdl.Archive{{
			URL: srv.URL + "/ffmpeg.zip", Format: "zip", SHA256: h(sha), Size: int64(len(body)),
			Members: []ffmpegdl.Member{
				{Entry: "build/bin/ffmpeg", Name: "ffmpeg", Size: int64(len(dlFFmpeg)), SHA256: hexSum([]byte(dlFFmpeg))},
				{Entry: "build/bin/ffprobe", Name: "ffprobe", Size: int64(len(dlFFprobe)), SHA256: hexSum([]byte(dlFFprobe))},
			},
		}},
	}
	r.svc = ffmpegdl.New(ffmpegdl.Options{
		Layout:  ffmpegdl.Layout{Root: filepath.Join(t.TempDir(), "ffmpeg")},
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): platform}},
		GOOS:    "linux", GOARCH: "amd64",
		Fetch: fetch.Options{Client: srv.Client(), CheckURL: func(*url.URL) error { return nil }},
		Check: func(context.Context, string) error { return nil },
	})
	t.Cleanup(r.svc.Close)
	return r
}

func (r *downloadRig) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gate != nil {
		select {
		case <-r.gate:
		default:
			close(r.gate)
		}
	}
}

// act sends an htmx request with the CSRF token.
func (s *ffmpegScreen) act(method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

func TestFFmpegDownload_NoPanelWhereTheAppOrThePlatformHasNoDownload(t *testing.T) {
	assert.NotContains(t, newFFmpegScreen(t).get(paths.FFmpegSettings()), `data-testid="ffmpeg-download"`, "no service")

	unsupported := ffmpegdl.New(ffmpegdl.Options{Catalog: ffmpegdl.Default(), GOOS: "linux", GOARCH: "amd64"})
	s := newFFmpegScreenWith(t, unsupported)
	assert.NotContains(t, s.get(paths.FFmpegSettings()), `data-testid="ffmpeg-download"`, "a platform with no pinned build keeps today's guidance")
	rec := s.act(http.MethodPost, paths.FFmpegDownload())
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "ダウンロードできません")
}

func TestFFmpegDownload_ThePanelSaysWhatIsFetchedFromWhereAndHowItIsChecked(t *testing.T) {
	s := newFFmpegScreenWith(t, newDownloadRig(t, "", false).svc)
	body := s.get(paths.FFmpegSettings())
	assert.Contains(t, body, `data-testid="ffmpeg-download-button"`)
	assert.Contains(t, body, "test build")
	assert.Contains(t, body, "Test Publisher")
	assert.Contains(t, body, "SHA-256")
	assert.Contains(t, body, "GPL v3")
	assert.Contains(t, body, "https://example.com/source")
	assert.Contains(t, body, "ボタンを押したときだけ通信します")
	assert.NotContains(t, body, `data-testid="ffmpeg-download-remove"`)
	assert.NotContains(t, body, `hx-trigger="every 2s"`, "it polls only while a download runs")
}

func TestFFmpegDownload_ButtonDownloadsAndTheAppFindsTheCopy(t *testing.T) {
	r := newDownloadRig(t, "", true)
	s := newFFmpegScreenWith(t, r.svc)
	assert.Contains(t, s.get(paths.FFmpegSettings()), "見つかりません")

	rec := s.act(http.MethodPost, paths.FFmpegDownload())
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="ffmpeg-download-progress"`)
	assert.Contains(t, body, `hx-get="/settings/ffmpeg/download"`)
	assert.Contains(t, body, `hx-trigger="every 2s"`, "the panel polls itself while the download runs")
	assert.Contains(t, body, `data-testid="ffmpeg-download-cancel"`)
	assert.NotContains(t, body, `data-testid="ffmpeg-download-button"`)

	again := s.act(http.MethodPost, paths.FFmpegDownload())
	assert.Equal(t, http.StatusOK, again.Code)
	assert.Contains(t, again.Body.String(), `data-testid="ffmpeg-download-progress"`, "a second click starts nothing")

	r.release()
	r.svc.WaitInstall()
	polled := s.act(http.MethodGet, paths.FFmpegDownload())
	require.Equal(t, http.StatusOK, polled.Code)
	body = polled.Body.String()
	assert.Contains(t, body, `data-testid="ffmpeg-download-done"`)
	assert.Contains(t, body, "起動し直してください")
	assert.Contains(t, body, `data-testid="ffmpeg-download-remove"`)
	assert.NotContains(t, body, `hx-trigger="every 2s"`, "it stops polling")

	page := s.get(paths.FFmpegSettings())
	assert.Contains(t, page, "使えます", "the lookup of the section finds the downloaded copy")
	assert.NotContains(t, page, "見つかりません")
	assert.Contains(t, page, filepath.Join(r.svc.Status().Dir, "ffmpeg"))
}

func TestFFmpegDownload_CancelKeepsNothingInstalledAndOffersTheButtonAgain(t *testing.T) {
	r := newDownloadRig(t, "", true)
	s := newFFmpegScreenWith(t, r.svc)
	require.Equal(t, http.StatusOK, s.act(http.MethodPost, paths.FFmpegDownload()).Code)
	rec := s.act(http.MethodPost, paths.FFmpegDownloadCancel())
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="ffmpeg-download-cancelled"`)
	assert.NotContains(t, body, `data-testid="ffmpeg-download-install-error"`, "cancel is not an error")
	assert.Contains(t, body, `data-testid="ffmpeg-download-button"`)
	assert.Empty(t, r.svc.Dirs())
}

func TestFFmpegDownload_AWrongChecksumIsShownAndTheCopyIsNotUsed(t *testing.T) {
	r := newDownloadRig(t, strings.Repeat("0", 64), false)
	s := newFFmpegScreenWith(t, r.svc)
	require.Equal(t, http.StatusOK, s.act(http.MethodPost, paths.FFmpegDownload()).Code)
	r.svc.WaitInstall()
	body := s.act(http.MethodGet, paths.FFmpegDownload()).Body.String()
	assert.Contains(t, body, `data-testid="ffmpeg-download-install-error"`)
	assert.Contains(t, body, "SHA-256")
	assert.Contains(t, body, "削除しました")
	assert.Contains(t, body, `data-testid="ffmpeg-download-button"`, "the user can try again")
	assert.NotContains(t, body, `data-testid="ffmpeg-download-done"`)
	assert.Contains(t, s.get(paths.FFmpegSettings()), "見つかりません", "a download that does not match is never found")
	assert.Empty(t, r.svc.Dirs())
}

func TestFFmpegDownload_RemoveDeletesTheCopy(t *testing.T) {
	r := newDownloadRig(t, "", false)
	s := newFFmpegScreenWith(t, r.svc)
	require.Equal(t, http.StatusOK, s.act(http.MethodPost, paths.FFmpegDownload()).Code)
	r.svc.WaitInstall()
	require.NotEmpty(t, r.svc.Dirs())

	rec := s.act(http.MethodDelete, paths.FFmpegDownload())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "削除しました")
	assert.Contains(t, rec.Body.String(), `data-testid="ffmpeg-download-button"`)
	assert.Empty(t, r.svc.Dirs())
	assert.Contains(t, s.get(paths.FFmpegSettings()), "見つかりません")
}

func TestFFmpegDownload_RoutesNeedTheCSRFToken(t *testing.T) {
	s := newFFmpegScreenWith(t, newDownloadRig(t, "", false).svc)
	for _, m := range []string{http.MethodPost, http.MethodDelete} {
		req := httptest.NewRequest(m, paths.FFmpegDownload(), nil)
		req.Header.Set("HX-Request", "true")
		req.AddCookie(s.cookie)
		assert.Equal(t, http.StatusForbidden, s.send(req).Code, m)
	}
}

func hexSum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
