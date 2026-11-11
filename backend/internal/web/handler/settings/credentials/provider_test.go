package credentials_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/credential/probe"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/credentials"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/credentials"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

type providerRig struct {
	engine  *gin.Engine
	keys    *credentialtest.Memory
	models  *modelsettings.Service
	path    string
	applied int
}

func newProviderRig(t *testing.T, pinned bool) *providerRig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := &providerRig{engine: gin.New(), keys: credentialtest.New(), path: filepath.Join(t.TempDir(), "models.json")}
	if pinned {
		require.NoError(t, os.WriteFile(r.path, []byte(`{"video_provider":"openrouter"}`), 0o600))
	}
	r.models = modelsettings.New(r.path, pinned)
	r.models.SetOnChange(func() { r.applied++ })
	k := credentials.Keys{Svc: service.NewCredentialService(r.keys), Tester: probe.NewTester(r.keys), Models: r.models}
	credentials.Register(r.engine, k, func(*gin.Context) {})
	r.engine.GET("/panel", func(c *gin.Context) {
		c.Header("Content-Type", "text/html")
		_ = view.Panel(credentials.Data(c, k)).Render(c.Request.Context(), c.Writer)
	})
	return r
}

func (r *providerRig) do(method, target string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.engine.ServeHTTP(rec, req)
	return rec
}

func (r *providerRig) panel() string { return r.do(http.MethodGet, "/panel", nil).Body.String() }

// providers lists the data-provider of the cards, in order.
func providers(body string) []string {
	var out []string
	for _, part := range strings.Split(body, `data-provider="`)[1:] {
		out = append(out, part[:strings.Index(part, `"`)])
	}
	return out
}

func TestPanel_TheProviderChoiceIsAtTheTopAndTheOfficialCardsStayByDefault(t *testing.T) {
	r := newProviderRig(t, false)

	body := r.panel()

	assert.Less(t, strings.Index(body, `data-testid="api-provider"`), strings.Index(body, `data-testid="credential-row"`), "the choice comes before every card")
	for _, v := range []string{"official", "openrouter", "fal", "replicate"} {
		assert.Contains(t, body, `value="`+v+`"`)
	}
	assert.Regexp(t, `value="official"[^>]*checked|checked[^>]*value="official"`, body)
	got := providers(body)
	assert.Contains(t, got, "kling")
	assert.Contains(t, got, "kling4")
	assert.NotContains(t, got, "openrouter", "the aggregators' keys are not asked for until one is chosen")
	assert.NotContains(t, got, "fal")
	assert.NotContains(t, got, "replicate")
	assert.Contains(t, body, `hx-put="/settings/api-provider"`)
}

func TestPanel_AnAggregatorShowsItsOwnKeyFirstAndHidesTheMakersKlingKeys(t *testing.T) {
	r := newProviderRig(t, false)
	require.NoError(t, r.models.SaveVideoProvider("fal"))

	got := providers(r.panel())

	require.NotEmpty(t, got)
	assert.Equal(t, "fal", got[0], "the provider's key field is right under the choice")
	assert.NotContains(t, got, "kling")
	assert.NotContains(t, got, "kling4")
	assert.NotContains(t, got, "openrouter")
	assert.Contains(t, got, "anthropic", "the keys of everything else are still there")
	assert.Contains(t, got, "seedance")
}

func TestPanel_ShowsPricesWhyNotAndAMissingKey(t *testing.T) {
	r := newProviderRig(t, false)
	require.NoError(t, r.models.SaveVideoProvider("openrouter"))

	body := r.panel()

	assert.Contains(t, body, "720p $0.084/秒")
	assert.Contains(t, body, "1080p $0.112/秒")
	assert.Contains(t, body, "OpenRouter の API キーが未設定", "a missing key is said next to the model that needs it")
	assert.Contains(t, body, "OpenRouter は Kling 4.0 を提供していません")
	assert.Contains(t, body, "このプロバイダーでは使えません")

	require.NoError(t, r.keys.Set("openrouter", "or-dummy-key-0123456789"))
	body = r.panel()
	assert.Contains(t, body, "OpenRouter の API キー: 設定済み")
	assert.NotContains(t, body, "or-dummy-key-0123456789")
}

func TestSaveProvider_SavesAppliesAtOnceAndAnswersWithThePanel(t *testing.T) {
	r := newProviderRig(t, false)

	rec := r.do(http.MethodPut, paths.APIProvider(), url.Values{"provider": {"replicate"}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, r.applied)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	body := rec.Body.String()
	assert.Contains(t, body, `id="credential-panel"`)
	assert.Contains(t, body, "API プロバイダーを保存しました")
	assert.Contains(t, providers(body), "replicate")
	assert.Contains(t, body, "720p $0.168/秒", "the price of Replicate, not of the maker")
	raw, err := os.ReadFile(r.path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"video_provider": "replicate"`)
}

func TestSaveProvider_AnUnknownProviderIs422AndNothingIsWritten(t *testing.T) {
	r := newProviderRig(t, false)

	rec := r.do(http.MethodPut, paths.APIProvider(), url.Values{"provider": {"midjourney"}})

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="credential-error"`)
	assert.Contains(t, rec.Body.String(), "公式・OpenRouter・fal.ai・Replicate")
	assert.NoFileExists(t, r.path)
	assert.Zero(t, r.applied)
}

func TestSaveProvider_APinnedFileIs409AndTheChoiceIsDisabled(t *testing.T) {
	r := newProviderRig(t, true)

	rec := r.do(http.MethodPut, paths.APIProvider(), url.Values{"provider": {"fal"}})

	require.Equal(t, http.StatusConflict, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "起動オプションで固定されているため、この画面では変更できません")
	assert.Contains(t, body, "disabled")
	assert.Contains(t, providers(body), "openrouter", "the pinned file's provider is still the one shown")
}

func TestPanel_AnUnreadableSettingsFileLeavesTheOfficialCardsAndSaysWhy(t *testing.T) {
	r := newProviderRig(t, false)
	require.NoError(t, os.WriteFile(r.path, []byte(`{not json`), 0o600))

	body := r.panel()

	assert.Contains(t, body, `data-testid="api-provider-problem"`)
	assert.Contains(t, providers(body), "kling")
}

func TestSaveProvider_AnUnreadableSettingsFileIsRefusedAndLeftAsItIs(t *testing.T) {
	r := newProviderRig(t, false)
	require.NoError(t, os.WriteFile(r.path, []byte(`{not json`), 0o600))

	rec := r.do(http.MethodPut, paths.APIProvider(), url.Values{"provider": {"fal"}})

	assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest)
	assert.Contains(t, rec.Body.String(), `data-testid="credential-error"`)
	assert.Contains(t, rec.Body.String(), `data-testid="api-provider-problem"`)
	assert.NotContains(t, rec.Body.String(), "API プロバイダーを保存しました")
	assert.Zero(t, r.applied)
	raw, err := os.ReadFile(r.path)
	require.NoError(t, err)
	assert.Equal(t, `{not json`, string(raw))
}

func TestTheChoiceIsLeftOutWithoutModelSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	keys := credentialtest.New()
	k := credentials.Keys{Svc: service.NewCredentialService(keys)}
	engine := gin.New()
	engine.GET("/panel", func(c *gin.Context) { _ = view.Panel(credentials.Data(c, k)).Render(c.Request.Context(), c.Writer) })
	req := httptest.NewRequest(http.MethodGet, "/panel", http.NoBody)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	assert.NotContains(t, rec.Body.String(), `data-testid="api-provider"`)
	assert.Contains(t, providers(rec.Body.String()), "kling")
}

// The list under the choice says whether the key of the chosen provider is stored, and reads the same key as the card: a save and a delete of that key redraw both.
func TestSaveAndDelete_RedrawTheKeyStateOfTheChosenProviderUnderTheChoice(t *testing.T) {
	r := newProviderRig(t, false)
	require.NoError(t, r.models.SaveVideoProvider("openrouter"))
	require.Contains(t, r.panel(), "OpenRouter の API キーが未設定")

	saved := r.do(http.MethodPut, paths.Credential("openrouter"), url.Values{"api_key": {"or-dummy-key-0123456789"}})

	require.Equal(t, http.StatusOK, saved.Code)
	assert.Regexp(t, `<ul id="api-provider-models"[^>]*hx-swap-oob="true"`, saved.Body.String(), "the list replaces the one on the page out of band")
	assert.Contains(t, saved.Body.String(), "OpenRouter の API キー: 設定済み")
	assert.NotContains(t, saved.Body.String(), "OpenRouter の API キーが未設定")
	assert.NotContains(t, saved.Body.String(), "or-dummy-key-0123456789")

	deleted := r.do(http.MethodDelete, paths.Credential("openrouter"), nil)

	require.Equal(t, http.StatusOK, deleted.Code)
	assert.Regexp(t, `<ul id="api-provider-models"[^>]*hx-swap-oob="true"`, deleted.Body.String())
	assert.Contains(t, deleted.Body.String(), "OpenRouter の API キーが未設定")
	assert.NotContains(t, deleted.Body.String(), "OpenRouter の API キー: 設定済み")
}

// Without model settings (Keys.Models is nil) the panel has no choice of the API provider and so no list of how it serves each model: nothing is sent out of band. (With model
// settings the list is sent, also under the official provider; the tests above.)
func TestSave_SendsNoModelListWhereThePanelHasNone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	keys := credentialtest.New()
	engine := gin.New()
	credentials.Register(engine, credentials.Keys{Svc: service.NewCredentialService(keys)}, func(*gin.Context) {})
	req := httptest.NewRequest(http.MethodPut, paths.Credential("anthropic"), strings.NewReader(url.Values{"api_key": {"sk-ant-dummy-0123456789"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "api-provider-models")
}
