package ffmpegscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

// TestMain starts the fake engine the model manager rig needs.
func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// ffmpegScreen is the page of the companion software of the model manager with the ffmpeg section, over a Mac whose PATH has no Homebrew (the Finder launch).
type ffmpegScreen struct {
	*screentest.Screen
	t    *testing.T
	file string
}

func newFFmpegScreen(t *testing.T) *ffmpegScreen { return newFFmpegScreenWith(t, nil) }

// newFFmpegScreenWith is the screen with the download of ffmpeg (nil: the app has none, as the headless server).
func newFFmpegScreenWith(t *testing.T, download *ffmpegdl.Service) *ffmpegScreen {
	t.Helper()
	var opts []ffmpegsettings.Option
	if download != nil {
		opts = append(opts, ffmpegsettings.WithManagedDirs(download.Dirs))
	}
	svc, file := screentest.IsolatedFFmpeg(t, opts...)
	return &ffmpegScreen{Screen: screentest.NewWithCompanion(t, screentest.Companion{FFmpeg: svc, Download: download}), t: t, file: file}
}

// get is the section as the page's htmx reads it (GET /model-manager/companion/ffmpeg); path is where.
func (s *ffmpegScreen) get(path string) string {
	rec := s.Get(path)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func (s *ffmpegScreen) put(form url.Values) *httptest.ResponseRecorder {
	return s.Do(http.MethodPut, paths.CompanionFFmpeg(), form)
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

	body := s.get(paths.CompanionFFmpeg())

	assert.Contains(t, body, `data-testid="ffmpeg-state-ffmpeg"`)
	assert.Contains(t, body, "見つかりません")
	assert.Contains(t, body, "/opt/homebrew/bin、/usr/local/bin", "the Homebrew directories are searched after PATH")
	assert.Contains(t, s.PageOf("companion"), `id="mm-ffmpeg"`, "the page of the companion software has the section")
}

func TestFFmpegSettings_SavedPathsAreShownAndWrittenAndClearedAgain(t *testing.T) {
	s := newFFmpegScreen(t)
	ffmpeg, ffprobe := executable(t, "myffmpeg"), executable(t, "myffprobe")

	rec := s.put(url.Values{"ffmpeg": {ffmpeg}, "ffprobe": {ffprobe}})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `id="ffmpeg-panel"`, "the answer is the section, to swap in place")
	assert.Contains(t, rec.Body.String(), "すぐに使われます")
	assert.Contains(t, rec.Body.String(), `value="`+ffmpeg+`"`)
	b, err := os.ReadFile(s.file) // #nosec G304 -- a file of the test's temp directory
	require.NoError(t, err)
	assert.Contains(t, string(b), ffmpeg)
	assert.Contains(t, s.get(paths.CompanionFFmpeg()), "使えます", "the next visit finds them with the saved paths")

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

	body := s.get(paths.CompanionFFmpeg())

	assert.Contains(t, body, `data-testid="ffmpeg-problem"`)
	assert.Contains(t, s.Page(), "ffmpeg: 設定ファイルを読めません", "the overview of the model manager says it")
	assert.Contains(t, s.PageOf("companion"), `data-testid="mm-companion-row" data-tool="ffmpeg" data-state="attention"`, "and so does the list")
}

// A path that cannot be written because the folder of the settings is read-only is told as the common error (#1010): the cause is the folder, the
// remedy says what to do, and neither is the generic sentence that every failed save used to give.
func TestFFmpegSettings_AReadOnlyFolderIsToldWithItsCauseAndRemedy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only folder")
	}
	s := newFFmpegScreen(t)
	dir := filepath.Dir(s.file)
	require.NoError(t, os.Chmod(dir, 0o500))       // #nosec G302 -- the test makes its own folder read-only
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // #nosec G302 -- so that the temporary directory can be removed

	rec := s.put(url.Values{"ffmpeg": {executable(t, "myffmpeg")}, "ffprobe": {executable(t, "myffprobe")}})

	assert.Equal(t, http.StatusInternalServerError, rec.Code, "the status of the class of save_failed")
	assert.Contains(t, rec.Body.String(), "設定やデータを保存できませんでした（保存先のフォルダに書き込めません）")
	assert.Contains(t, rec.Body.String(), "フォルダの権限を確かめてから")
	assert.NotContains(t, rec.Body.String(), "permission denied", "the English message of the system stays in the log")
}
