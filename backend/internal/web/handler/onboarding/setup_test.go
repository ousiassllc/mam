package onboarding_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// What the credentials step asks for is one API key or one ChatGPT sign-in. Its state line says which; it is not decided
// from anything but the stores themselves.
func TestCredentialsStep_TheStateLineSaysWhetherAKeyOrALoginIsSet(t *testing.T) {
	keys := credentialtest.New()
	r := newRig(t, func(c *config) { c.keys = keys })

	body := r.page(paths.OnboardingCredentials)
	assert.Contains(t, body, `id="onboarding-credentials-status"`)
	assert.Contains(t, body, "API キーも ChatGPT ログインもまだありません")
	assert.Contains(t, body, `hx-get="`+paths.OnboardingCredentialsStatus()+`"`)
	assert.Contains(t, body, `hx-trigger="modal-close from:body, htmx:afterRequest[detail.requestConfig.verb!='get'] from:body"`,
		"it reads itself again when a modal closes and when a request that is not a GET ends (a key deleted), but never after its own GET")
	assert.NotContains(t, body, "htmx:afterSettle", "afterSettle would fire again after the line's own reload, without end")
	assert.Contains(t, body, `id="credential-row-openai"`, "the cards of the settings screen are the ones in the step")
	assert.Contains(t, body, `hx-get="`+paths.CredentialEdit("openai")+`"`)
	assert.Contains(t, body, `id="mam-modal"`, "the page holds the modal the cards fill")

	require.Equal(t, http.StatusOK, r.act(http.MethodPut, "/credentials/openai", url.Values{"api_key": {"sk-test-onboarding"}}).Code)

	status := r.get(paths.OnboardingCredentialsStatus())
	assert.Equal(t, http.StatusOK, status.Code)
	assert.Equal(t, "no-store", status.Header().Get("Cache-Control"))
	assert.Contains(t, status.Body.String(), "設定済み: API キー 1 件")
	assert.NotContains(t, status.Body.String(), "sk-test-onboarding", "the key is never drawn")
	assert.Contains(t, r.page(paths.OnboardingCredentials), "設定済み: API キー 1 件")
}

// A ChatGPT sign-in is as good as a key: the state line is "設定済み" once the login that the card started has succeeded
// (the fake OAuth server of the ChatGPT tests; no real account).
func TestCredentialsStep_AChatGPTLoginCountsLikeAKey(t *testing.T) {
	r := newRig(t, func(c *config) { c.chatGPT = true })
	assert.Contains(t, r.page(paths.OnboardingCredentials), `data-testid="chatgpt-login-official"`, "the login cards of the settings screen")
	assert.Contains(t, r.get(paths.OnboardingCredentialsStatus()).Body.String(), "まだありません")

	require.Equal(t, http.StatusOK, r.act(http.MethodPost, paths.ChatGPTLogin("official"), url.Values{"method": {"browser"}}).Code)

	require.Eventually(t, func() bool {
		return strings.Contains(r.get(paths.OnboardingCredentialsStatus()).Body.String(), "設定済み: ChatGPT ログイン済み")
	}, 10*time.Second, 10*time.Millisecond)
}

// A sign-in that cannot be read (the OS key store does not answer) is not "no sign-in": the line says it cannot tell,
// like it does for the API keys.
func TestCredentialsStep_AChatGPTSignInThatCannotBeReadIsSaidSoNotTakenForNone(t *testing.T) {
	broken := credentialtest.New()
	broken.FailWith(errors.New("locked"))
	r := newRig(t, func(c *config) { c.chatGPT, c.chatGPTKeys = true, broken })

	for name, body := range map[string]string{"line": r.get(paths.OnboardingCredentialsStatus()).Body.String(), "step": r.page(paths.OnboardingCredentials)} {
		assert.Contains(t, body, "ChatGPT ログインの状態を確認できません", name)
		assert.NotContains(t, body, "まだありません", name)
	}
}

// The OS key store can be slow: the step reads the keys once for the state line and the cards together, and the line alone
// reads them once as well.
func TestCredentialsStep_ReadsTheKeyStoreOncePerRequest(t *testing.T) {
	counted := &countingStore{Store: credentialtest.New()}
	r := newRig(t, func(c *config) { c.keys = counted })
	providers := len(service.CredentialProviders)

	counted.n.Store(0)
	r.page(paths.OnboardingCredentials)
	assert.EqualValues(t, providers, counted.n.Load(), "one read of each provider for the whole step")

	counted.n.Store(0)
	r.get(paths.OnboardingCredentialsStatus())
	assert.EqualValues(t, providers, counted.n.Load(), "and for the state line alone")
}

// countingStore counts the lookups of a key: one per provider and read.
type countingStore struct {
	credential.Store
	n atomic.Int64
}

func (s *countingStore) Has(account string) (bool, error) {
	s.n.Add(1)
	return s.Store.Has(account)
}

func TestCredentialsStep_AKeyStoreThatCannotBeReadIsSaidSo(t *testing.T) {
	keys := credentialtest.New()
	keys.FailWith(errors.New("locked"))
	r := newRig(t, func(c *config) { c.keys = keys })

	body := r.get(paths.OnboardingCredentialsStatus()).Body.String()

	assert.Contains(t, body, "状態を確認できません")
	assert.NotContains(t, body, "設定済み")
}

// The key typed in the guide goes where the settings screen puts it: the OS key store, service "mam", account
// "api-key:<provider>" (here the in-memory mock of go-keyring), by the route of the settings screen.
func TestCredentialsStep_AKeySavedHereLandsInTheOSKeyStoreWhereTheSettingsPutIt(t *testing.T) {
	keyring.MockInit()
	r := newRig(t, func(c *config) { c.keys = credential.NewKeyring() })

	rec := r.act(http.MethodPut, "/credentials/anthropic", url.Values{"api_key": {"sk-ant-onboarding"}})

	require.Equal(t, http.StatusOK, rec.Code)
	got, err := keyring.Get("mam", "api-key:anthropic")
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-onboarding", got)
	assert.Contains(t, r.get(paths.OnboardingCredentialsStatus()).Body.String(), "設定済み: API キー 1 件")
}

// The models step embeds the tables of the settings screen; a save there is the settings' own PUT and writes llm.json.
func TestModelsStep_SavesWhereTheSettingsScreenSaves(t *testing.T) {
	r := newRig(t)

	body := r.page(paths.OnboardingModels)
	assert.Contains(t, body, "既定の割り当てのままでも進めます")
	assert.Contains(t, body, `data-testid="onboarding-llm"`)
	assert.Contains(t, body, `data-testid="onboarding-media"`)
	assert.Contains(t, body, `data-testid="llm-restart-note"`, "the table of the LLM category")
	assert.Contains(t, body, `data-testid="model-assignment"`, "the tables of the image / video models category")
	assert.Contains(t, body, `hx-get="`+paths.LLMAgentEdit("director")+`"`)
	assert.NotContains(t, body, `data-testid="onboarding-llm" open`, "folded: the defaults work")
	require.NoFileExists(t, r.llmFile)

	rec := r.act(http.MethodPut, paths.LLMAgent("director"), url.Values{
		"primary.provider": {"google"}, "primary.model": {"gemini-onboarding"}, "fallback.provider": {"openai"}, "fallback.model": {"gpt-x"},
	})

	require.Equal(t, http.StatusOK, rec.Code)
	raw, err := os.ReadFile(r.llmFile)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "gemini-onboarding")
}

// The macOS step is only there on macOS. It says where ComfyUI comes from (Comfy Desktop, not bundled) and takes the codex
// path with the setting of the settings screen.
func TestMacStep_IsForMacOSAndPointsToComfyDesktopWithoutBundlingComfyUI(t *testing.T) {
	r := newRig(t, func(c *config) { c.goos, c.chatGPT = "darwin", true })

	body := r.page(paths.OnboardingMac)

	assert.Equal(t, "はじめに: Mac の準備（Codex・ComfyUI） | mam", titleRe.FindStringSubmatch(body)[1])
	assert.Contains(t, body, "ステップ 7 / 7")
	assert.Contains(t, body, "ComfyUI は外部のものを使います")
	assert.Contains(t, body, "Comfy Desktop")
	assert.Contains(t, body, "mam は ComfyUI を同梱しません")
	assert.Contains(t, body, `href="https://docs.comfy.org/installation/desktop/macos"`)
	assert.Contains(t, body, "macOS 13 以降")
	assert.Contains(t, body, `href="`+paths.LocalModels()+`"`, "the address goes into the local models settings")
	assert.Contains(t, body, `data-testid="onboarding-codex-path"`)
	assert.Contains(t, body, `hx-put="`+paths.OnboardingCodexPath()+`"`)
	assert.Contains(t, body, `data-testid="onboarding-finish"`, "the macOS step is the last one")
	assert.Contains(t, r.page(paths.OnboardingPrepare), "Mac の準備")
}

func TestMacStep_WithoutChatGPTThereIsNoCodexPath(t *testing.T) {
	r := newRig(t, func(c *config) { c.goos = "darwin" })

	body := r.page(paths.OnboardingMac)

	assert.NotContains(t, body, "onboarding-codex-path")
	assert.Contains(t, body, "Comfy Desktop")
	assert.Equal(t, http.StatusNotFound, r.act(http.MethodPut, paths.OnboardingCodexPath(), url.Values{"path": {"/x"}}).Code)
}

func TestCodexPath_SetsClearsAndRefusesLikeTheSettingsScreen(t *testing.T) {
	r := newRig(t, func(c *config) { c.goos, c.chatGPT = "darwin", true })
	exe := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(exe, []byte("not a real executable"), 0o700))

	rec := r.act(http.MethodPut, paths.OnboardingCodexPath(), url.Values{"path": {exe}})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `role="status"`)
	assert.Equal(t, exe, r.settings.ChatGPT.Codex().Status().CustomPath, "the setting the settings screen's field writes")
	assert.Contains(t, r.page(paths.OnboardingMac), `value="`+exe+`"`)

	for name, bad := range map[string]string{"relative": "bin/codex", "missing": filepath.Join(t.TempDir(), "missing")} {
		rec = r.act(http.MethodPut, paths.OnboardingCodexPath(), url.Values{"path": {bad}})
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, name)
		assert.Contains(t, rec.Body.String(), `role="alert"`, name)
		assert.Equal(t, exe, r.settings.ChatGPT.Codex().Status().CustomPath, "a refused path keeps the one that was set")
	}

	rec = r.act(http.MethodPut, paths.OnboardingCodexPath(), url.Values{"path": {""}})
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "解除しました")
	assert.Empty(t, r.settings.ChatGPT.Codex().Status().CustomPath)
}

// A path that is fine but cannot be saved is not the user's mistake: it is 500 (with the reason for the log), not the 422
// that tells the user to name an existing file.
func TestCodexPath_APathThatCannotBeSavedIsNotTheUsersMistake(t *testing.T) {
	r := newRig(t, func(c *config) { c.goos, c.chatGPT = "darwin", true })
	exe := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(exe, []byte("not a real executable"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(r.chatGPTDir, codex.StateFile), 0o700)) // a directory where the record belongs

	rec := r.act(http.MethodPut, paths.OnboardingCodexPath(), url.Values{"path": {exe}})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), `role="alert"`)
	assert.Contains(t, rec.Body.String(), "保存できませんでした")
	assert.NotContains(t, rec.Body.String(), "絶対パスを指定してください")
}

// The route rewrites the settings of the executable mam runs: like every write it needs the CSRF token.
func TestCodexPath_NeedsTheCSRFToken(t *testing.T) {
	r := newRig(t, func(c *config) { c.goos, c.chatGPT = "darwin", true })
	exe := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(exe, []byte("not a real executable"), 0o700))

	req := httptest.NewRequest(http.MethodPut, paths.OnboardingCodexPath(), strings.NewReader(url.Values{"path": {exe}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(r.cookie) // the cookie alone is not the token
	rec := r.send(req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, r.settings.ChatGPT.Codex().Status().CustomPath, "nothing was set")
}

func TestCodexPath_IsNotThereOutsideMacOS(t *testing.T) {
	r := newRig(t, func(c *config) { c.goos, c.chatGPT = "linux", true })

	assert.Equal(t, http.StatusNotFound, r.act(http.MethodPut, paths.OnboardingCodexPath(), url.Values{"path": {"/x"}}).Code)
}
