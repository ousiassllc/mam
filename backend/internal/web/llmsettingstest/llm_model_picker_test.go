package llmsettingstest_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
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

// optionIDs are the values of the options of the <select> named name.
func optionIDs(t *testing.T, html, name string) []string {
	t.Helper()
	sel := regexp.MustCompile(`(?s)<select[^>]*name="` + regexp.QuoteMeta(name) + `"[^>]*>.*?</select>`).FindString(html)
	require.NotEmpty(t, sel, name)
	var out []string
	for _, m := range regexp.MustCompile(`<option value="([^"]*)"`).FindAllStringSubmatch(sel, -1) {
		out = append(out, m[1])
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

// With no API key and nobody signed in, every provider's pull-down offers the candidates of mam, says so, and says why.
func TestLLMSettings_WithoutKeysThePullDownOffersMamsCandidatesAndSaysSo(t *testing.T) {
	s := newLLMScreen(t, false)
	body := s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil).Body.String()
	def := llm.DefaultConfig().Routes[llm.AgentDirector]

	assert.Equal(t, llm.CandidateModels(def.Primary.Provider), optionIDs(t, body, "primary.model"))
	assert.Contains(t, body, "出所: mam の既定候補（API キーが登録されていません）")
	assert.Contains(t, body, `<optgroup label="mam の既定候補">`)
	assert.NotContains(t, body, "使えないかもしれません", "the shipped default is in the list")
	assert.Regexp(t, `<select[^>]*id="llm-primary-model"[^>]*>`, body)
	assert.NotRegexp(t, `<input[^>]*name="primary.model"`, body, "no free text box under the model name")
	provider := regexp.MustCompile(`<select[^>]*name="primary.provider"[^>]*>`).FindString(body)
	assert.Contains(t, provider, `hx-get="`+paths.LLMModelField("director", "primary")+`"`, "a change of the provider asks for its models")
	assert.Contains(t, provider, `hx-target="#llm-primary-model-field"`)
	assert.Contains(t, body, `hx-disabled-elt="#llm-save"`, "the save waits for the models of the new provider")
}

// A model that is saved but not in the list stays chosen, with a warning, and the save is not refused.
func TestLLMSettings_ASavedModelTheListLacksIsWarnedAndStillSaves(t *testing.T) {
	s := newLLMScreen(t, false)
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("openai", "gpt-from-the-future", "google", "gemini-x")).Code)

	body := s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil).Body.String()
	assert.Equal(t, "gpt-from-the-future", selectedValue(t, body, "primary.model"))
	assert.Contains(t, body, `<optgroup label="保存済み（一覧にありません）">`)
	assert.Regexp(t, `data-testid="llm-primary-model-unlisted"[^>]*>「gpt-from-the-future」は openai の一覧にありません。使えないかもしれません`, body)
	assert.Contains(t, body, `data-testid="llm-fallback-model-unlisted"`, "gemini-x too")

	// The listed models are still offered next to it.
	assert.Subset(t, optionIDs(t, body, "primary.model"), llm.CandidateModels(llm.ProviderOpenAI))
}

// The name typed under "一覧にないモデル名" wins over the pull-down's, so a model that no list has can still be used.
func TestLLMSettings_ATypedNameWinsOverThePullDown(t *testing.T) {
	s := newLLMScreen(t, false)
	form := agentForm("openai", llm.CandidateModels(llm.ProviderOpenAI)[0], "google", llm.CandidateModels(llm.ProviderGoogle)[0])
	form.Set("primary.model_custom", "  gpt-typed  ")
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), form).Code)

	cfg, err := llm.LoadConfig(s.file)
	require.NoError(t, err)
	assert.Equal(t, "gpt-typed", cfg.Routes[llm.AgentDirector].Primary.Model)
	assert.Equal(t, llm.CandidateModels(llm.ProviderGoogle)[0], cfg.Routes[llm.AgentDirector].Fallback.Model)
}

// A change of provider answers the model box of the new provider alone: the saved model stays chosen only for the provider it
// was saved for, else the default's (when it is that provider's) or the first model.
func TestLLMSettings_ChangingTheProviderAnswersTheModelBoxOfTheNewOne(t *testing.T) {
	s := newLLMScreen(t, false)
	def := llm.DefaultConfig().Routes[llm.AgentDirector]
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("openai", "gpt-saved", string(def.Fallback.Provider), def.Fallback.Model)).Code)

	ask := func(role, provider string) (int, string) {
		rec := s.requestTo(http.MethodGet, paths.LLMModelField("director", role)+"?"+url.Values{role + ".provider": {provider}}.Encode(), nil)
		return rec.Code, rec.Body.String()
	}

	code, body := ask("primary", "openai")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "gpt-saved", selectedValue(t, body, "primary.model"), "the provider it was saved for")
	assert.NotContains(t, body, "<html")

	code, body = ask("primary", "anthropic")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, llm.CandidateModels(llm.ProviderAnthropic), optionIDs(t, body, "primary.model"))
	assert.Equal(t, def.Primary.Model, selectedValue(t, body, "primary.model"), "the default's model is that provider's")
	assert.NotContains(t, body, "gpt-saved")

	code, body = ask("primary", "google")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, llm.CandidateModels(llm.ProviderGoogle)[0], selectedValue(t, body, "primary.model"))

	code, _ = ask("primary", "nobody")
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	code, _ = ask("middle", "openai")
	assert.Equal(t, http.StatusNotFound, code)
	rec := s.requestTo(http.MethodGet, paths.LLMModelField("nobody", "primary")+"?primary.provider=openai", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// With a key the models come from the provider, marked as such; a saved model that the provider does not list is warned.
func TestLLMSettings_WithAKeyThePullDownOffersTheProvidersListAndNeverShowsTheKey(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-live-b","created":1},{"id":"gpt-live-a","created":2}]}`))
	}))
	t.Cleanup(api.Close)
	s := llmScreenWithModels(t, api.URL)
	def := llm.DefaultConfig().Routes[llm.AgentDirector]
	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("openai", "gpt-live-a", string(def.Fallback.Provider), def.Fallback.Model)).Code)

	body := s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil).Body.String()
	assert.Equal(t, []string{"gpt-live-a", "gpt-live-b"}, optionIDs(t, body, "primary.model"))
	assert.Equal(t, "gpt-live-a", selectedValue(t, body, "primary.model"))
	assert.Contains(t, body, "出所: プロバイダーの API の一覧")
	assert.NotContains(t, body, "sk-test")
	assert.NotContains(t, strings.Join(optionIDs(t, body, "primary.model"), ","), "gpt-5")

	require.Equal(t, http.StatusOK, s.requestTo(http.MethodPut, paths.LLMAgent("director"), agentForm("openai", "gpt-5", string(def.Fallback.Provider), def.Fallback.Model)).Code)
	body = s.requestTo(http.MethodGet, paths.LLMAgentEdit("director"), nil).Body.String()
	assert.Equal(t, "gpt-5", selectedValue(t, body, "primary.model"))
	assert.Contains(t, body, `data-testid="llm-primary-model-unlisted"`, "gpt-5 is not in this account's list")
}
