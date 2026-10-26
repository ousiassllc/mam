package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/service/updater"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// scriptedChecker answers the update checks with status; it counts them.
type scriptedChecker struct {
	status updater.Status
	err    error
	calls  atomic.Int32
}

func (c *scriptedChecker) CheckForUpdate(context.Context) (updater.Result, error) {
	c.calls.Add(1)
	return updater.Result{}, c.err
}

func (c *scriptedChecker) Status() updater.Status { return c.status }

// updateScreen is the settings screen with the update service over checker (nil: the headless server).
type updateScreen struct {
	t      *testing.T
	engine http.Handler
	cookie *http.Cookie
	token  string
	page   string
}

func newUpdateScreen(t *testing.T, version string, checker appupdate.Checker) *updateScreen {
	t.Helper()
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)
	if checker != nil {
		settings.Update = appupdate.New(checker, version, func(string) {}, nil)
	}
	s := &updateScreen{t: t, engine: router.New(svc, router.Options{Settings: settings})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.SettingsCategory(paths.SettingsUpdate), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			s.cookie = c
		}
	}
	require.NotNil(t, s.cookie)
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token, s.page = m[1], rec.Body.String()
	return s
}

func (s *updateScreen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *updateScreen) check(withToken bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, paths.UpdateCheck(), nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	if withToken {
		req.Header.Set(middleware.CSRFHeaderName, s.token)
	}
	return s.send(req)
}

// The headless server has no updater: the section shows the version (a build from a working tree is
// "開発版") and offers no check, and the check route does not exist.
func TestUpdateSettings_WithoutAnUpdaterTheSectionShowsTheVersionOnly(t *testing.T) {
	s := newUpdateScreen(t, "", nil)

	assert.Contains(t, s.page, "バージョンとアップデート")
	assert.Contains(t, s.page, `id="update-panel"`)
	assert.Regexp(t, `data-testid="update-version">開発版<`, s.page)
	assert.NotContains(t, s.page, paths.UpdateCheck())
	assert.NotContains(t, s.page, "今すぐ確認")
	assert.Contains(t, s.page, "自動更新に対応していません")
	assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, s.check(true).Code)
}

// The screen shows the running version, how the last check ended and the button that checks now; the
// form drops a second submit while one is in flight (the rule of every posting form).
func TestUpdateSettings_TheScreenShowsTheVersionTheLastCheckAndTheButton(t *testing.T) {
	checked := time.Date(2026, 10, 21, 3, 4, 0, 0, time.UTC)
	s := newUpdateScreen(t, "v0.1.2", &scriptedChecker{status: updater.Status{CheckedAt: checked}})

	assert.Regexp(t, `data-testid="update-version">v0\.1\.2<`, s.page)
	assert.Contains(t, s.page, "最新のバージョンです。")
	assert.Contains(t, s.page, `datetime="2026-10-21T03:04:00Z"`)
	form := regexp.MustCompile(`<form[^>]*hx-post="` + paths.UpdateCheck() + `"[^>]*>`).FindString(s.page)
	require.NotEmpty(t, form)
	assert.Contains(t, form, `hx-sync="this:drop"`)
	assert.Contains(t, form, `hx-disabled-elt="find button[type=submit]"`)
	assert.Contains(t, form, `hx-target="#update-panel"`)
	assert.Regexp(t, `<button[^>]*type="submit"[^>]*>今すぐ確認</button>`, s.page)
}

func TestUpdateSettings_CheckNowRunsACheckAndAnswersWithTheSection(t *testing.T) {
	checker := &scriptedChecker{status: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.2.0", Blocked: true, BlockedKind: updater.BlockMedia}}
	s := newUpdateScreen(t, "v0.1.2", checker)

	rec := s.check(true)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.EqualValues(t, 1, checker.calls.Load())
	body := rec.Body.String()
	assert.NotContains(t, body, "<html", "the answer is the section, not a page")
	assert.Contains(t, body, `id="update-panel"`)
	assert.Contains(t, body, "新しいバージョン v0.2.0 があります")
	assert.Contains(t, body, "動画の変換（ffmpeg）が動いているため")
	assert.Contains(t, body, `role="alert"`, "a held-back update is announced")
}

func TestUpdateSettings_AFailedCheckShowsItsKindNotTheRawError(t *testing.T) {
	checker := &scriptedChecker{
		err:    assert.AnError,
		status: updater.Status{CheckedAt: time.Now(), LastError: "secret internal detail", ErrorKind: updater.ErrorVerification},
	}
	s := newUpdateScreen(t, "v0.1.2", checker)

	rec := s.check(true)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "ダウンロードしたファイルを検証できませんでした")
	assert.Contains(t, body, `data-tone="error"`)
	assert.NotContains(t, body, "secret internal detail")
}

func TestUpdateSettings_ADevBuildSaysItDoesNotCheck(t *testing.T) {
	s := newUpdateScreen(t, "dev", &scriptedChecker{status: updater.Status{CheckedAt: time.Now(), DevBuild: true}})

	rec := s.check(true)

	assert.Contains(t, rec.Body.String(), "開発版のため、更新は確認しません。")
	assert.Contains(t, rec.Body.String(), `data-testid="update-version">開発版<`)
}

// The check changes the state (it may end in a restart), so a request without the CSRF token is refused
// and runs nothing.
func TestUpdateSettings_CheckNowNeedsTheCSRFToken(t *testing.T) {
	checker := &scriptedChecker{status: updater.Status{CheckedAt: time.Now()}}
	s := newUpdateScreen(t, "v0.1.2", checker)

	rec := s.check(false)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, checker.calls.Load())
}
