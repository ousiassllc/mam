package ffmpegscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// ffmpegScreen is the settings screen with the ffmpeg section, over a Mac whose PATH has no Homebrew (the Finder launch).
type ffmpegScreen struct {
	t      *testing.T
	file   string
	engine http.Handler
	cookie *http.Cookie
	token  string
}

func newFFmpegScreen(t *testing.T) *ffmpegScreen { return newFFmpegScreenWith(t, nil) }

// newFFmpegScreenWith is the screen with the download of ffmpeg (nil: the app has none, as the headless server).
func newFFmpegScreenWith(t *testing.T, download *ffmpegdl.Service) *ffmpegScreen {
	t.Helper()
	file := filepath.Join(t.TempDir(), ffmpegsettings.FileName)
	// Only the files of the temporary directory count: the machine's own ffmpeg, if it has one, is not found.
	finder := locate.Finder{GOOS: "darwin", Path: "/usr/bin:/bin", Usable: func(p string) bool {
		return strings.HasPrefix(p, os.TempDir()) && locate.IsExecutable(p)
	}}
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)
	opts := []ffmpegsettings.Option{ffmpegsettings.WithFinder(finder)}
	if download != nil {
		settings.FFmpegDownload = download
		opts = append(opts, ffmpegsettings.WithManagedDirs(download.Dirs))
	}
	settings.FFmpeg = ffmpegsettings.New(file, opts...)
	s := &ffmpegScreen{t: t, file: file, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

func (s *ffmpegScreen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *ffmpegScreen) get(path string) string {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(s.cookie)
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func (s *ffmpegScreen) put(form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, paths.FFmpegSettings(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// executable writes an executable file and returns its path.
func executable(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"), 0o600))
	require.NoError(t, os.Chmod(p, 0o700)) // #nosec G302 -- the test file has to be executable
	return p
}

func TestFFmpegSettings_PageSaysWhereItSearchesAndThatNothingIsFound(t *testing.T) {
	s := newFFmpegScreen(t)

	body := s.get(paths.FFmpegSettings())

	assert.Contains(t, body, `data-testid="ffmpeg-state-ffmpeg"`)
	assert.Contains(t, body, "見つかりません")
	assert.Contains(t, body, "/opt/homebrew/bin、/usr/local/bin", "the Homebrew directories are searched after PATH")
	assert.Contains(t, s.get(paths.Settings()), `href="/settings/ffmpeg"`, "the overview has the card")
}

func TestFFmpegSettings_SavedPathsAreShownAndWrittenAndClearedAgain(t *testing.T) {
	s := newFFmpegScreen(t)
	ffmpeg, ffprobe := executable(t, "myffmpeg"), executable(t, "myffprobe")

	rec := s.put(url.Values{"ffmpeg": {ffmpeg}, "ffprobe": {ffprobe}})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="ffmpeg-panel"`, "the answer is the section, to swap in place")
	assert.Contains(t, rec.Body.String(), "次に mam を起動したときから使われます")
	assert.Contains(t, rec.Body.String(), `value="`+ffmpeg+`"`)
	b, err := os.ReadFile(s.file) // #nosec G304 -- a file of the test's temp directory
	require.NoError(t, err)
	assert.Contains(t, string(b), ffmpeg)
	assert.Contains(t, s.get(paths.FFmpegSettings()), "使えます", "the next visit finds them with the saved paths")

	rec = s.put(url.Values{"ffmpeg": {""}, "ffprobe": {""}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "パスの指定を解除しました")
	assert.NoFileExists(t, s.file)
}

func TestFFmpegSettings_APathThatCannotBeUsedIsRefusedWithItsReasonAndKeptInTheField(t *testing.T) {
	s := newFFmpegScreen(t)
	missing := filepath.Join(t.TempDir(), "nope", "ffmpeg")

	rec := s.put(url.Values{"ffmpeg": {missing}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="ffmpeg-error"`)
	assert.Contains(t, rec.Body.String(), "実在する実行ファイルではありません")
	assert.Contains(t, rec.Body.String(), `value="`+missing+`"`, "what was typed stays")
	assert.NoFileExists(t, s.file)

	rec = s.put(url.Values{"ffmpeg": {"ffmpeg"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "絶対パスではありません")
}

func TestFFmpegSettings_ADamagedFileIsTheProblemOfTheSectionOnly(t *testing.T) {
	s := newFFmpegScreen(t)
	require.NoError(t, os.WriteFile(s.file, []byte("{"), 0o600))

	body := s.get(paths.FFmpegSettings())

	assert.Contains(t, body, `data-testid="ffmpeg-problem"`)
	assert.Contains(t, s.get(paths.Settings()), "設定ファイルを読めません")
}
