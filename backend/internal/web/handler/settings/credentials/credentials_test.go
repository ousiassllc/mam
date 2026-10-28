package credentials_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

const secret = "sk-typed-secret-0123456789"

type rig struct {
	engine *gin.Engine
	keys   *credentialtest.Memory
	svc    *service.CredentialService
	tester *probe.Tester
	page   int // how many times the page handler answered
}

func newRig(t *testing.T) *rig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := &rig{engine: gin.New(), keys: credentialtest.New()}
	r.svc = service.NewCredentialService(r.keys)
	r.tester = probe.NewTester(r.keys)
	credentials.Register(r.engine, credentials.Keys{Svc: r.svc, Tester: r.tester}, func(c *gin.Context) { r.page++; c.String(http.StatusOK, "the category page") })
	return r
}

func (r *rig) do(method, target string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	r.engine.ServeHTTP(rec, req)
	return rec
}

// The modal of a provider says what the key is for, has one password field that is never filled in, and keeps the key out
// of everything that is drawn, whether or not one is saved.
func TestEdit_TheModalHasAnEmptyPasswordFieldAndNeverTheStoredKey(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.svc.Save("kling", secret))

	rec := r.do(http.MethodGet, paths.CredentialEdit("kling"), nil, true)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `id="mam-modal-title"`)
	assert.Contains(t, body, "Kling の API キー")
	assert.Contains(t, body, "動画生成（Kling 3.0")
	assert.Contains(t, body, `type="password"`)
	assert.Contains(t, body, `autocomplete="off"`)
	assert.Contains(t, body, `name="api_key"`)
	assert.Contains(t, body, `maxlength="2560"`)
	assert.Contains(t, body, `autofocus`)
	assert.NotContains(t, body, `value="`, "no value is ever written into the key field")
	assert.NotContains(t, body, secret)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Contains(t, body, "新しいキーを入力して保存すると、保存済みのキーを置き換えます", "a provider that has a key says what a save does")
	assert.NotContains(t, body, "空のまま保存", "an empty save is refused with 422, so the copy never promises one")
	assert.NotContains(t, body, "接続テストは、空のままなら", "kling has no connection test, so it does not say what an empty test does")
	assert.Contains(t, body, `hx-put="/credentials/kling"`)
	assert.Contains(t, body, `hx-sync="this:drop"`)
	assert.Contains(t, body, `hx-disabled-elt="find button[type=submit]"`)
}

// Only the providers with a free read-only call get a "接続テスト"; the others say that the first generation is the test.
func TestEdit_OnlyProvidersWithAFreeReadOnlyCallHaveAConnectionTest(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"anthropic", "openai", "google", "flux", "runway", "elevenlabs"} {
		body := r.do(http.MethodGet, paths.CredentialEdit(p), nil, true).Body.String()
		assert.Contains(t, body, `hx-post="/settings/credentials/`+p+`/test"`, p)
		assert.NotContains(t, body, `data-testid="credential-no-test"`, p)
	}
	for _, p := range []string{"kling", "kling4", "minimax", "alibaba", "seedance", "synclabs", "typesafe"} {
		body := r.do(http.MethodGet, paths.CredentialEdit(p), nil, true).Body.String()
		assert.NotContains(t, body, "/test", p)
		assert.Contains(t, body, "保存したあと、最初の生成で確かめられます", p)
	}
}

// A provider with a connection test says what an empty field means there (the saved key is used); one with nothing
// saved says nothing of it.
func TestEdit_SaysThatAnEmptyConnectionTestUsesTheSavedKey(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.keys.Set("openai", secret))

	assert.Contains(t, r.do(http.MethodGet, paths.CredentialEdit("openai"), nil, true).Body.String(), "接続テストは、空のままなら保存済みのキーで試します")
	assert.NotContains(t, r.do(http.MethodGet, paths.CredentialEdit("anthropic"), nil, true).Body.String(), "保存済みのキー", "nothing is saved for it")
}

func TestEdit_UnknownProviderIs404WithAModalThatSaysSoAndANavigationGetsThePage(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodGet, paths.CredentialEdit("nobody"), nil, true)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "対象が見つかりません")
	assert.Contains(t, rec.Body.String(), "data-modal-close", "the modal can be closed")

	rec = r.do(http.MethodGet, paths.CredentialEdit("kling"), nil, false)
	assert.Equal(t, "the category page", rec.Body.String(), "typed into the address bar, it is the page of its category")
	assert.Equal(t, 1, r.page)
}

// A save answers with the card replaced out of band and the event that closes the modal: no form, no key.
func TestSave_ReplacesTheCardOutOfBandAndClosesTheModal(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodPut, paths.Credential("kling"), url.Values{"api_key": {"  " + secret + "  "}}, true)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Equal(t, "modal-close", rec.Header().Get("HX-Trigger-After-Swap"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Contains(t, body, `<li id="credential-row-kling"`)
	assert.Contains(t, body, `hx-swap-oob="true"`)
	assert.Contains(t, body, "設定済み")
	assert.Contains(t, body, `<div id="toast-region" hx-swap-oob="beforeend">`, "what was done is told in a toast, which outlives the modal")
	assert.Contains(t, body, "API キーを保存しました")
	assert.NotContains(t, body, secret)
	assert.NotContains(t, body, `id="mam-modal-content"`, "nothing goes into the modal's body")
	got, err := r.keys.Get("kling")
	require.NoError(t, err)
	assert.Equal(t, secret, got)
}

// A refused key is the modal again, with why: the field to blame is marked, and the typed key is not written back.
func TestSave_RefusedKeyIs422WithTheModalAgainAndNoKeyInIt(t *testing.T) {
	r := newRig(t)
	for _, bad := range []string{"", "two words"} {
		rec := r.do(http.MethodPut, paths.Credential("kling"), url.Values{"api_key": {bad}}, true)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%q", bad)
		body := rec.Body.String()
		assert.Empty(t, rec.Header().Get("HX-Trigger-After-Swap"), "the modal stays open")
		assert.Contains(t, body, `id="mam-modal-error" role="alert" tabindex="-1"`)
		assert.Contains(t, body, `aria-invalid="true"`)
		assert.Contains(t, body, `aria-describedby="mam-modal-error"`)
		assert.Contains(t, body, `autofocus`)
		assert.NotContains(t, body, `value="`+bad+`"`)
	}
	rec := r.do(http.MethodPut, paths.Credential("kling"), url.Values{"api_key": {"has two words"}}, true)
	assert.NotContains(t, rec.Body.String(), "has two words", "a refused key is never echoed")
	ok, err := r.keys.Has("kling")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSave_UnknownProviderIsAToastAndSavesNothing(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodPut, paths.Credential("nobody"), url.Values{"api_key": {secret}}, true)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "data-toast")
	ok, err := r.keys.Has("nobody")
	require.NoError(t, err)
	assert.False(t, ok)
}

// A delete asks the card to replace itself; deleting a key that is not set is fine.
func TestDelete_AnswersTheCardAloneAndIsIdempotent(t *testing.T) {
	r := newRig(t)
	require.NoError(t, r.svc.Save("kling", secret))

	for range 2 {
		rec := r.do(http.MethodDelete, paths.Credential("kling"), nil, true)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `<li id="credential-row-kling"`)
		assert.NotRegexp(t, `<li[^>]*hx-swap-oob`, rec.Body.String(), "the card is the answer, not an out-of-band extra")
		assert.Contains(t, rec.Body.String(), "API キーを削除しました")
		assert.Contains(t, rec.Body.String(), "未設定")
	}
	assert.Equal(t, http.StatusUnprocessableEntity, r.do(http.MethodDelete, paths.Credential("nobody"), nil, true).Code)
}

// The connection test answers in the modal's status line whatever the provider said, and keeps the key out of the answer,
// the store and the URL.
func TestTest_SaysWhatTheProviderAnsweredAndSavesNothing(t *testing.T) {
	var seenKey, seenPath string
	status := http.StatusOK
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKey, seenPath = r.Header.Get("Authorization"), r.URL.RequestURI()
		w.WriteHeader(status)
	}))
	t.Cleanup(provider.Close)
	r := newRig(t)
	r.tester.Prober = probe.Prober{BaseURLs: map[string]string{"openai": provider.URL}}

	tests := []struct {
		status int
		want   string
	}{
		{http.StatusOK, "接続できました"},
		{http.StatusUnauthorized, "キーが拒否されました（この確認に必要な権限が足りない場合もあります）"},
		{http.StatusForbidden, "権限を絞ったキーは、保存して最初の生成で確かめてください"},
		{http.StatusTooManyRequests, "上限に達しています"},
		{http.StatusInternalServerError, "通信できませんでした"},
		{http.StatusNotFound, "想定外の応答でした（HTTP 404）"},
	}
	for _, tc := range tests {
		status = tc.status
		rec := r.do(http.MethodPost, paths.CredentialTest("openai"), url.Values{"api_key": {secret}}, true)

		assert.Equal(t, http.StatusOK, rec.Code, "an answer of the provider is not a failure of the screen")
		assert.Contains(t, rec.Body.String(), tc.want)
		assert.NotContains(t, rec.Body.String(), secret)
		assert.Equal(t, "Bearer "+secret, seenKey)
		assert.NotContains(t, seenPath, secret)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
	ok, err := r.keys.Has("openai")
	require.NoError(t, err)
	assert.False(t, ok, "a test never saves")
}

func TestTest_RefusesWhatCannotBeTried(t *testing.T) {
	r := newRig(t)

	rec := r.do(http.MethodPost, paths.CredentialTest("kling"), url.Values{"api_key": {secret}}, true)
	assert.Equal(t, http.StatusNotFound, rec.Code, "no free read-only call for Kling")

	rec = r.do(http.MethodPost, paths.CredentialTest("openai"), nil, true)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no typed key and no saved one")
	assert.Contains(t, rec.Body.String(), "API キーを入力してください")
}

// Kling 3.0 and Kling 4.0 have a key each, and each says which form of key it takes: ACCESS_KEY:SECRET_KEY for the old API,
// one API key for the new one (spec/architecture/kling/kling-api.md §5).
func TestEdit_Kling3AndKling4SayWhichFormOfKeyTheyTake(t *testing.T) {
	r := newRig(t)

	kling3 := r.do(http.MethodGet, paths.CredentialEdit("kling"), nil, true).Body.String()
	assert.Contains(t, kling3, "ACCESS_KEY:SECRET_KEY")
	assert.Contains(t, kling3, "Kling 4.0 は次の「Kling 4.0」に API キーを入れる")

	kling4 := r.do(http.MethodGet, paths.CredentialEdit("kling4"), nil, true).Body.String()
	assert.Contains(t, kling4, "Kling 4.0 の API キー")
	assert.Contains(t, kling4, "Kling の API キー 1 つ")
	assert.Contains(t, kling4, `hx-put="/credentials/kling4"`)
}
