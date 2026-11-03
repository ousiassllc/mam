package llmsettingstest_test

import (
	"encoding/json"
	htmlpkg "html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/router"
	"github.com/ousiassllc/mam/backend/internal/router/settingsroutes"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// pickerModels are the models the picker lists as cards (provider/model, in order): each card carries what choosing it sets in the field.
func pickerModels(t *testing.T, html string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`data-mp-values="([^"]*)"`).FindAllStringSubmatch(html, -1) {
		var v map[string]string
		require.NoError(t, json.Unmarshal([]byte(htmlpkg.UnescapeString(m[1])), &v))
		out = append(out, v["provider"]+"/"+v["model"])
	}
	return out
}

// shownProviders are the providers with at least one model card.
func shownProviders(t *testing.T, html string) []string {
	t.Helper()
	var out []string
	for _, m := range pickerModels(t, html) {
		p, _, _ := strings.Cut(m, "/")
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// llmScreenWithModels is the settings screen whose model lists come from the OpenAI-shaped server at base (and the saved key).
func llmScreenWithModels(t *testing.T, base string) *llmScreen {
	t.Helper()
	keys := credentialtest.New()
	require.NoError(t, keys.Set("openai", "sk-test"))
	file := filepath.Join(t.TempDir(), "data", "llm.json")
	svc := servicetest.New(t, service.Deps{DB: testdb.Open(t), Credentials: credentialtest.New()})
	st := settingsroutes.New(file, false, filepath.Join(t.TempDir(), "models.json"), false)
	st.LLMModels = llmmodels.New(llmmodels.Options{Keys: keys, BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: base}})
	s := &llmScreen{t: t, file: file, engine: router.New(svc, router.Options{Settings: st})}
	rec := s.send(httptest.NewRequest(http.MethodGet, paths.Settings(), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		s.cookie = c
	}
	m := regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2)
	s.token = m[1]
	return s
}

const pickerURL = "/model-picker/settings-llm"

// With no API key and nobody signed in, the picker starts at "選べるものだけ": no provider has a model to choose, it says how many cannot be chosen now, and "使えないものも表示"
// lists them with why (#1013) and where it is fixed.
func TestLLMSettings_WithoutKeysThePickerHidesTheModelsAndShowsThemWithTheReasonOnRequest(t *testing.T) {
	s := newLLMScreen(t, false)

	body := s.requestTo(http.MethodGet, pickerURL, nil).Body.String()
	assert.Empty(t, pickerModels(t, body), "no key, no sign-in: nothing can be chosen now")
	assert.Contains(t, body, "件は今は選べません")
	assert.NotContains(t, body, "sk-")

	body = s.requestTo(http.MethodGet, pickerURL+"?mf=1&available_only=0", nil).Body.String()
	def := llm.DefaultConfig().Routes[llm.AgentDirector]
	assert.Contains(t, pickerModels(t, body), string(def.Primary.Provider)+"/"+def.Primary.Model, "the shipped default is listed, but not usable")
	assert.Contains(t, body, "API キーが登録されていません")
	assert.Contains(t, body, `href="/settings/api-keys"`, "the way to the setting that fixes it")
	assert.Contains(t, body, "選べません")
}

// The modal of an Agent is the two model fields, each posting the provider and the model it holds; a provider without a key says so, with the way to the setting.
func TestLLMSettings_TheModalSaysWhichSavedProviderCannotBeUsed(t *testing.T) {
	s := newLLMScreen(t, false)
	body := s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil).Body.String()
	def := llm.DefaultConfig().Routes[llm.AgentDirector]

	assert.Equal(t, string(def.Primary.Provider), hiddenValue(t, body, "primary.provider"))
	assert.Equal(t, def.Primary.Model, hiddenValue(t, body, "primary.model"))
	assert.Equal(t, 2, strings.Count(body, `hx-get="`+pickerURL+`"`))
	assert.Contains(t, body, `data-testid="llm-primary-unusable"`)
	assert.Contains(t, body, `href="/settings/api-keys"`)
	assert.Contains(t, body, "（保存はできます）")
}

// The assignment screen warns of a saved route whose provider cannot be used; the route stays (#1013).
func TestLLMSettings_TheScreenWarnsOfASavedRouteThatCannotBeUsed(t *testing.T) {
	s := newLLMScreen(t, false)
	body := s.page()

	assert.Contains(t, body, `data-testid="llm-warnings"`)
	assert.Contains(t, body, "使えません")
	assert.Contains(t, body, `href="/settings/api-keys"`)
}

// A model that is saved but not in the list stays chosen, with a warning, and the save is not refused.
func TestLLMSettings_ASavedModelTheListLacksIsWarnedAndStillSaves(t *testing.T) {
	s := newLLMScreen(t, false)
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("openai", "gpt-from-the-future", "google", "gemini-x")).Code)

	body := s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil).Body.String()
	assert.Equal(t, "gpt-from-the-future", hiddenValue(t, body, "primary.model"))
	assert.Regexp(t, `data-testid="llm-primary-model-unlisted"[^>]*>「gpt-from-the-future」は openai の一覧にありません。使えないかもしれません`, body)
	assert.Contains(t, body, `data-testid="llm-fallback-model-unlisted"`, "gemini-x too")
}

// The name typed under "一覧にないモデル名" wins over the picker's, so a model that no list has can still be used.
func TestLLMSettings_ATypedNameWinsOverThePicker(t *testing.T) {
	s := newLLMScreen(t, false)
	form := agentForm("openai", llm.CandidateModels(llm.ProviderOpenAI)[0], "google", llm.CandidateModels(llm.ProviderGoogle)[0])
	form.Set("primary.model_custom", "  gpt-typed  ")
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), form).Code)

	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	assert.Equal(t, "gpt-typed", cfg.Routes[llm.AgentDirector].Primary.Model)
	assert.Equal(t, llm.CandidateModels(llm.ProviderGoogle)[0], cfg.Routes[llm.AgentDirector].Fallback.Model)
}

// With a key the picker lists the models of that provider only, from the provider's own list, and never shows the key; the other providers have no models to choose.
func TestLLMSettings_WithAKeyThePickerOffersTheProvidersListAndNeverShowsTheKey(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-live-b","created":1},{"id":"gpt-live-a","created":2}]}`))
	}))
	t.Cleanup(api.Close)
	s := llmScreenWithModels(t, api.URL)

	body := s.requestTo(http.MethodGet, pickerURL, nil).Body.String()
	assert.Equal(t, []string{"openai/gpt-live-a", "openai/gpt-live-b"}, pickerModels(t, body))
	assert.Equal(t, []string{"openai"}, shownProviders(t, body), "the providers without a key are hidden")
	assert.NotContains(t, body, "sk-test")

	// A saved model the provider's list lacks stays chosen, with a warning.
	def := llm.DefaultConfig().Routes[llm.AgentDirector]
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("openai", "gpt-5", string(def.Fallback.Provider), def.Fallback.Model)).Code)
	body = s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil).Body.String()
	assert.Equal(t, "gpt-5", hiddenValue(t, body, "primary.model"))
	assert.Contains(t, body, `data-testid="llm-primary-model-unlisted"`, "gpt-5 is not in this account's list")
	assert.NotContains(t, body, `data-testid="llm-primary-unusable"`, "openai has a key")
}
