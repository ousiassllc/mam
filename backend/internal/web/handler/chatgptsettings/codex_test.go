package chatgptsettings_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// codexScreen is the settings screen over a fake Codex app-server, with an executable the user "installed".
func codexScreen(t *testing.T) (*screen, *codextest.Fake) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home) // nothing may be read or written below the user's own home
	t.Setenv("USERPROFILE", home)
	var fake *codextest.Fake
	s := newScreen(t, true, func(o *chatgpt.Options) {
		fake = codextest.New(t, filepath.Join(o.DataDir, "codex-home"))
		fake.Models = []protocol.Model{{ID: "m1", Model: "gpt-vision", DisplayName: "Vision", InputModalities: []string{"text", "image"}}}
		reset, mins := int64(1_792_000_000), int64(300) // 2026-10-14 17:46:40 UTC → 02:46 on the 15th in Japan
		fake.RateLimits = protocol.RateLimitsResult{RateLimits: protocol.RateLimitSnapshot{
			Primary: &protocol.RateLimitWindow{UsedPercent: 42, WindowDurationMins: &mins, ResetsAt: &reset},
		}}
		o.Codex = func(co *codex.Options) {
			co.Start = func(appserver.Launch) (appserver.Process, error) { return fake.Process(), nil }
		}
	})
	exe := filepath.Join(t.TempDir(), "codex-app-server-test")
	require.NoError(t, os.WriteFile(exe, []byte("not a real executable"), 0o700))
	require.Equal(t, http.StatusOK, s.act(http.MethodPut, paths.ChatGPTCodexPath(), url.Values{"path": {exe}}).Code)
	return s, fake
}

func TestChatGPTCodex_TheOfficialBlockIsTheDefaultAndTheTabShowsTheTermsAndTheEngine(t *testing.T) {
	s := newScreen(t, true)
	assert.Contains(t, s.page, `data-testid="chatgpt-method-codex"`)
	assert.NotContains(t, s.page, `data-testid="chatgpt-codex-terms"`)

	req := httptest.NewRequest(http.MethodGet, paths.ChatGPTSettings()+"?method=codex", nil)
	rec := s.send(req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "商用・ホスティング")
	assert.Contains(t, body, `data-testid="chatgpt-codex-engine"`)
	assert.Contains(t, body, `data-testid="chatgpt-codex-unsupported"`, "the download is for Windows; here only a path")
	assert.Contains(t, body, `data-testid="chatgpt-codex-path"`)
	assert.NotContains(t, body, `data-testid="chatgpt-codex-install-button"`)
	assert.NotContains(t, body, `data-testid="chatgpt-login-codex"`, "no login before there is an engine")
}

func TestChatGPTCodex_InstallIsRefusedWhereTheDownloadIsNotAvailable(t *testing.T) {
	s := newScreen(t, true)
	rec := s.act(http.MethodPost, paths.ChatGPTCodexInstall(), nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "ダウンロードできません")
	assert.Equal(t, http.StatusOK, s.act(http.MethodPost, paths.ChatGPTCodexInstallCancel(), nil).Code)
}

func TestChatGPTCodex_ThePathNeedsAFileAndCanBeCleared(t *testing.T) {
	s := newScreen(t, true)
	rec := s.act(http.MethodPut, paths.ChatGPTCodexPath(), url.Values{"path": {"relative/codex"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "絶対パス")
	rec = s.act(http.MethodPut, paths.ChatGPTCodexPath(), url.Values{"path": {filepath.Join(t.TempDir(), "missing")}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	exe := filepath.Join(t.TempDir(), "codex.exe")
	require.NoError(t, os.WriteFile(exe, []byte("x"), 0o700))
	rec = s.act(http.MethodPut, paths.ChatGPTCodexPath(), url.Values{"path": {exe}})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "パスを設定しました")
	assert.Contains(t, s.panel(), "準備済み", "an engine is there, so the Codex block is the one shown")

	rec = s.act(http.MethodPut, paths.ChatGPTCodexPath(), url.Values{"path": {""}})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "解除しました")
}

func TestChatGPTCodex_LoginRefreshModelsAndLogout(t *testing.T) {
	s, fake := codexScreen(t)

	body := s.panel()
	assert.Contains(t, body, `data-testid="chatgpt-login-codex-browser"`)
	assert.Contains(t, body, `data-testid="chatgpt-login-codex-device"`)
	assert.NotContains(t, body, `data-testid="chatgpt-codex-models-show"`, "models only for a signed-in account")

	rec := s.act(http.MethodPost, paths.ChatGPTLogin("codex"), url.Values{"method": {"browser"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ブラウザで ChatGPT にログインしてください")

	l, ok := s.svc.Codex().CurrentLogin()
	require.True(t, ok)
	fake.Notify(protocol.NotifLoginCompleted, map[string]any{"success": true, "loginId": l.ID})
	body = s.waitFor(`data-testid="chatgpt-codex-assign-hint"`)
	assert.Contains(t, body, "user@example.invalid")
	assert.Contains(t, body, "プラン: business")
	assert.Contains(t, body, "chatgpt-codex")
	s.noTokens(body)

	rec = s.act(http.MethodPost, paths.ChatGPTCodexRefresh(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "5 時間: 42% 使用（リセット 2026-10-15 02:46（日本時間））")

	req := httptest.NewRequest(http.MethodGet, paths.ChatGPTCodexModels(), nil)
	req.Header.Set("HX-Request", "true")
	rec = s.send(req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "gpt-vision")
	assert.Contains(t, rec.Body.String(), "画像可")

	rec = s.act(http.MethodDelete, paths.ChatGPTCodex(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ログアウトしました")
	assert.NotContains(t, rec.Body.String(), "user@example.invalid")
	assert.Len(t, fake.CallsOf(protocol.MethodLogout), 1)
}

func TestChatGPTCodex_LoginWithoutAnEngineAsksForOne(t *testing.T) {
	s := newScreen(t, true)
	rec := s.act(http.MethodPost, paths.ChatGPTLogin("codex"), url.Values{"method": {"device"}})
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "先に Codex をダウンロード")
}

func TestChatGPTCodex_ActionsNeedTheCSRFToken(t *testing.T) {
	s := newScreen(t, true)
	for _, r := range []struct{ method, path string }{
		{http.MethodPost, paths.ChatGPTCodexInstall()}, {http.MethodPost, paths.ChatGPTCodexRefresh()},
		{http.MethodPut, paths.ChatGPTCodexPath()}, {http.MethodDelete, paths.ChatGPTCodex()},
		{http.MethodPost, paths.ChatGPTLogin("codex")},
	} {
		req := httptest.NewRequest(r.method, r.path, nil)
		req.AddCookie(s.cookie)
		assert.Equal(t, http.StatusForbidden, s.send(req).Code, r.method+" "+r.path)
	}
}
