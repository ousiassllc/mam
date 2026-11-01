package chatgptsettings_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
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

// The three login systems are three cards of one list, in the order of the spec; the Codex card replaced the tab.
func TestChatGPTCodex_TheCodexCardShowsTheTermsAndTheEngineAmongTheOtherCards(t *testing.T) {
	s := newScreen(t, true)
	assert.NotContains(t, s.page, `role="tablist"`, "the method tabs are gone: one card for each method")
	assert.NotContains(t, s.page, `data-testid="chatgpt-method-codex"`)
	official, image, codexCard := strings.Index(s.page, `id="chatgpt-card-official"`), strings.Index(s.page, `id="chatgpt-card-image"`), strings.Index(s.page, `id="chatgpt-card-codex"`)
	assert.True(t, 0 < official && official < image && image < codexCard, "official, image generation, Codex")

	body := s.panel()
	assert.Contains(t, body, "商用・ホスティング")
	assert.Contains(t, body, `data-testid="chatgpt-codex-engine"`)
	assert.Contains(t, body, `data-testid="chatgpt-codex-unsupported"`, "no download on this platform; here only a path")
	assert.Contains(t, body, `data-testid="chatgpt-codex-path"`)
	assert.NotContains(t, body, `data-testid="chatgpt-codex-install-button"`)
	assert.NotContains(t, body, `data-testid="chatgpt-login-codex"`, "no login before there is an engine")
	assert.Contains(t, body, "エンジン未取得")
}

func TestChatGPTCodex_InstallIsRefusedWhereTheDownloadIsNotAvailable(t *testing.T) {
	s := newScreen(t, true)
	rec := s.act(http.MethodPost, paths.ChatGPTCodexInstall(), nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "ダウンロードできません")
	assert.Contains(t, rec.Body.String(), "Windows 版と macOS 版（Apple Silicon）だけ", "the refusal names the platforms that can download")
	assert.NotContains(t, rec.Body.String(), "Windows 版だけです")
	assert.Equal(t, http.StatusOK, s.act(http.MethodPost, paths.ChatGPTCodexInstallCancel(), nil).Code)
}

// On macOS (Apple Silicon) the download is offered like on Windows, and a place that cannot be written to is named in the refusal.
func TestChatGPTCodex_TheDownloadIsOfferedOnMacOSArm64AndAnUnwritablePlaceIsNamed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	root := filepath.Join(file, "codex") // below a file: cannot be created
	s := newScreen(t, true, func(o *chatgpt.Options) {
		o.Codex = func(co *codex.Options) {
			co.Platform, co.Arch, co.Layout = "darwin", "arm64", engine.Layout{Root: root}
		}
	})
	body := s.panel()
	assert.Contains(t, body, `data-testid="chatgpt-codex-install-button"`)
	assert.NotContains(t, body, `data-testid="chatgpt-codex-unsupported"`)
	assert.Contains(t, body, "約 79 MB。展開後は約 201 MB。")
	assert.Contains(t, body, "置き場所は "+root)

	rec := s.act(http.MethodPost, paths.ChatGPTCodexInstall(), nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "書き込めません")
	assert.Contains(t, rec.Body.String(), root)
}

// An Intel Mac has no build to download: the screen says so and offers only the path.
func TestChatGPTCodex_NoDownloadOnMacOSIntel(t *testing.T) {
	s := newScreen(t, true, func(o *chatgpt.Options) {
		o.Codex = func(co *codex.Options) {
			co.Platform, co.Arch, co.Layout = "darwin", "amd64", engine.Layout{Root: filepath.Join(t.TempDir(), "codex")}
		}
	})
	body := s.panel()
	assert.Contains(t, body, `data-testid="chatgpt-codex-unsupported"`)
	assert.NotContains(t, body, `data-testid="chatgpt-codex-install-button"`)
	rec := s.act(http.MethodPost, paths.ChatGPTCodexInstall(), nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "ダウンロードできません")
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
	assert.Contains(t, body, `data-testid="chatgpt-login-codex"`)
	assert.NotContains(t, body, `data-testid="chatgpt-models-show-codex"`, "models only for a signed-in account")
	modal := s.modal(paths.ChatGPTLogin("codex"))
	assert.Contains(t, modal, `data-testid="chatgpt-login-codex-browser"`)
	assert.Contains(t, modal, `data-testid="chatgpt-login-codex-device"`)

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
