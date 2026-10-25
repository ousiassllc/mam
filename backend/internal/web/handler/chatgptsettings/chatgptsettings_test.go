package chatgptsettings_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

type screen struct {
	t      *testing.T
	engine http.Handler
	cookie *http.Cookie
	token  string
	page   string
	auth   *chatgpttest.Server
	keys   *credentialtest.Memory
	svc    *chatgpt.Service
	clock  atomic.Int64 // seconds the clock of the service is ahead
}

func newScreen(t *testing.T, withChatGPT bool, tweak ...func(*chatgpt.Options)) *screen {
	t.Helper()
	s := &screen{t: t, auth: chatgpttest.NewServer(t), keys: credentialtest.New()}
	s.keys.LimitBytes(2560)
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	settings := settingsroutes.New(filepath.Join(t.TempDir(), "llm.json"), false, filepath.Join(t.TempDir(), "models.json"), false)
	if withChatGPT {
		opts := chatgpt.Options{
			Keys: s.keys, DataDir: t.TempDir(), LockPath: filepath.Join(t.TempDir(), ".auth-refresh.lock"), Issuer: s.auth.URL,
			Now:   func() time.Time { return time.Now().Add(time.Duration(s.clock.Load()) * time.Second) },
			Sleep: func(context.Context, time.Duration) error { return nil },
			Open:  func(u string) error { s.auth.Approve(u); return nil },
		}
		for _, f := range tweak {
			f(&opts)
		}
		s.svc = chatgpt.New(opts)
		settings.ChatGPT = s.svc
	}
	s.engine = router.New(svc, router.Options{Settings: settings})
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.SettingsCategory(paths.SettingsAPIKeys), nil))
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

func (s *screen) send(req *http.Request) *httptest.ResponseRecorder {
	req.Host, req.RemoteAddr = "127.0.0.1:8787", "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	s.engine.ServeHTTP(rec, req)
	return rec
}

func (s *screen) act(method, target string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(s.cookie)
	req.Header.Set(middleware.CSRFHeaderName, s.token)
	return s.send(req)
}

// modal is the body of the modal at target, asked for as the button of the page does.
func (s *screen) modal(target string) string {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("HX-Request", "true")
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func (s *screen) panel() string {
	req := httptest.NewRequest(http.MethodGet, paths.ChatGPTSettings(), nil)
	req.Header.Set("HX-Request", "true")
	rec := s.send(req)
	require.Equal(s.t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// waitFor polls the panel until it holds want.
func (s *screen) waitFor(want string) string {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p := s.panel(); strings.Contains(p, want) {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.FailNow(s.t, "the panel never showed", want)
	return ""
}

func (s *screen) noTokens(html string) {
	s.t.Helper()
	for _, marker := range []string{"-ACCESS-", "-REFRESH-", "secretsecret", "eyJ"} {
		assert.NotContains(s.t, html, marker)
	}
}

func TestChatGPTSettings_WithoutTheServiceThereIsNoSectionAndNoRoute(t *testing.T) {
	s := newScreen(t, false)
	assert.NotContains(t, s.page, "ChatGPT でログイン")
	rec := s.act(http.MethodPost, paths.ChatGPTLogin("official"), url.Values{"method": {"browser"}})
	assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code)
}

func TestChatGPTSettings_TheSectionShowsTheButtonTheRisksAndAnOffSwitch(t *testing.T) {
	s := newScreen(t, true)
	for _, want := range []string{
		"ChatGPT でログイン", `data-testid="chatgpt-login-official"`, "ログインしていません", "chatgpt-subscription",
		"非公式", "サポート外", "利用上限", "オンにする", `data-testid="chatgpt-image-ack"`, "オフの間は、この経路を一切呼びません",
	} {
		assert.Contains(t, s.page, want)
	}
	assert.NotContains(t, s.page, `data-testid="chatgpt-login-image-device"`, "no image login while it is off")
	assert.NotContains(t, s.page, `data-testid="chatgpt-using-plan"`, "nothing to show before signing in")

	// The login is a modal: the button opens it, and the modal has the Continue with ChatGPT button.
	assert.Contains(t, s.page, `hx-get="`+paths.ChatGPTLogin("official")+`"`)
	modal := s.modal(paths.ChatGPTLogin("official"))
	assert.Contains(t, modal, "Continue with ChatGPT")
	assert.Contains(t, modal, `id="mam-modal-title"`)
}

func TestChatGPTSettings_OfficialLoginShowsTheAccountAndNeverATokenThenLogsOut(t *testing.T) {
	s := newScreen(t, true)
	rec := s.act(http.MethodPost, paths.ChatGPTLogin("official"), url.Values{"method": {"browser"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`, "the panel polls while the login is pending")

	p := s.waitFor("me@example.com")
	assert.Contains(t, p, "ログイン済み")
	assert.Contains(t, p, "Using ChatGPT plan")
	assert.Contains(t, p, "Manage usage")
	assert.Contains(t, p, "https://chatgpt.com/settings/usage")
	assert.Contains(t, p, "プラン: plus")
	assert.NotContains(t, p, `hx-trigger="every 2s"`, "no polling once it is done")
	s.noTokens(p)
	s.noTokens(s.page)
	full := s.send(httptest.NewRequest(http.MethodGet, paths.SettingsCategory(paths.SettingsAPIKeys), nil)).Body.String()
	assert.Contains(t, full, "me@example.com")
	s.noTokens(full)

	rec = s.act(http.MethodDelete, paths.ChatGPTAccount("official"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ログアウトしました")
	assert.NotContains(t, rec.Body.String(), "失効を確認できませんでした")
	assert.Contains(t, rec.Body.String(), "ログインしていません")
	assert.Equal(t, 1, s.auth.Count("/api/accounts/oauth/revoke"))

	s.act(http.MethodPost, paths.ChatGPTLogin("official"), url.Values{"method": {"browser"}})
	s.waitFor("me@example.com")
	s.auth.RevokeStatus = http.StatusServiceUnavailable
	rec = s.act(http.MethodDelete, paths.ChatGPTAccount("official"), nil)
	assert.Contains(t, rec.Body.String(), "失効を確認できませんでした", "the user is told when revocation is unconfirmed")
	assert.Contains(t, rec.Body.String(), "ログインしていません", "and the local tokens are gone")
}

func TestChatGPTSettings_AnExpiredSignInAsksToLogInAgain(t *testing.T) {
	s := newScreen(t, true)
	s.act(http.MethodPost, paths.ChatGPTLogin("official"), url.Values{"method": {"browser"}})
	s.waitFor("me@example.com")

	s.auth.RefreshHandler = func(w http.ResponseWriter, _ url.Values) bool {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":"invalid_grant"}`)
		return true
	}
	s.clock.Add(2 * 3600)
	_, err := s.svc.Official().Credentials(context.Background())
	require.ErrorIs(t, err, chatgpt.ErrReloginRequired)

	p := s.panel()
	assert.Contains(t, p, "再ログインが必要")
	assert.Contains(t, p, "ログインの有効期限が切れました")
	assert.Contains(t, p, "ログインし直す")
	s.noTokens(p)
}

func TestChatGPTSettings_ImageGenerationNeedsTheConfirmationAndHasItsOwnLogin(t *testing.T) {
	g := newGate(t)
	s := newScreen(t, true, g.tweak)
	rec := s.act(http.MethodPost, paths.ChatGPTLogin("image"), url.Values{"method": {"device"}})
	assert.Equal(t, http.StatusConflict, rec.Code, "no image login while it is off")
	assert.Zero(t, s.auth.Total())

	rec = s.act(http.MethodPut, paths.ChatGPTImageEnabled(), url.Values{"enabled": {"true"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "リスクを確認する")
	assert.False(t, s.svc.ImageEnabled(), "refused without the confirmation")

	rec = s.act(http.MethodPut, paths.ChatGPTImageEnabled(), url.Values{"enabled": {"true"}, "acknowledge_risks": {"true"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, s.svc.ImageEnabled())
	assert.Contains(t, rec.Body.String(), "オフにする")
	assert.Contains(t, rec.Body.String(), `data-testid="chatgpt-login-image"`)
	modal := s.modal(paths.ChatGPTLogin("image"))
	for _, want := range []string{"デバイスコードでログイン", "ブラウザでログイン"} {
		assert.Contains(t, modal, want, "the image login has both methods")
	}

	s.auth.PendingPolls = 1
	rec = s.act(http.MethodPost, paths.ChatGPTLogin("image"), url.Values{"method": {"device"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ABCD-1234", "the user code")
	assert.Contains(t, rec.Body.String(), s.auth.URL+"/codex/device", "the verification URL")
	g.open() // the poll interval ends: the login can finish
	s.waitFor("codex@example.com")
	p := s.panel()
	assert.NotContains(t, p, "ABCD-1234")
	s.noTokens(p)

	rec = s.act(http.MethodPut, paths.ChatGPTImageEnabled(), url.Values{"enabled": {"false"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, s.svc.ImageEnabled())
	assert.Contains(t, rec.Body.String(), "オフの間は、この経路を一切呼びません")

	rec = s.act(http.MethodDelete, paths.ChatGPTAccount("image"), nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, s.auth.Count("/oauth/revoke"))
}

func TestChatGPTSettings_OfficialHasNoDeviceLoginAndUnknownKindsAreRefused(t *testing.T) {
	s := newScreen(t, true)
	rec := s.act(http.MethodPost, paths.ChatGPTLogin("official"), url.Values{"method": {"device"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "デバイスコードの方式がありません")
	assert.NotContains(t, s.page, `data-testid="chatgpt-login-official-device"`)

	assert.Equal(t, http.StatusNotFound, s.act(http.MethodPost, paths.ChatGPTLogin("nobody"), url.Values{"method": {"browser"}}).Code)
	assert.Equal(t, http.StatusNotFound, s.act(http.MethodDelete, paths.ChatGPTAccount("nobody"), nil).Code)
}

func TestChatGPTSettings_ActionsNeedTheCSRFToken(t *testing.T) {
	s := newScreen(t, true)
	req := httptest.NewRequest(http.MethodDelete, paths.ChatGPTAccount("official"), nil)
	req.AddCookie(s.cookie)
	assert.Equal(t, http.StatusForbidden, s.send(req).Code)
}
