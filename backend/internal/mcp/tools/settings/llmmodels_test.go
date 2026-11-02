package settings_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

const apiKey = "sk-never-in-an-answer"

// withModels serves the tools over a fake OpenAI whose list holds gpt-live; asks counts the reads of the list.
func withModels(t *testing.T, saved bool) (*mcptest.Env, *atomic.Int32) {
	t.Helper()
	var asks atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asks.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-live","created":2}]}`))
	}))
	t.Cleanup(srv.Close)
	keys := credentialtest.New()
	if saved {
		require.NoError(t, keys.Set("openai", apiKey))
	}
	s, _ := newFiles(t, false)
	s.LLMModels = llmmodels.New(llmmodels.Options{Keys: keys, HTTP: srv.Client(), BaseURLs: map[llm.Provider]string{llm.ProviderOpenAI: srv.URL}})
	return newEnv(t, s), &asks
}

func TestListLLMModels_ReturnsTheProvidersListOrMamsCandidatesAndNeverTheKey(t *testing.T) {
	env, asks := withModels(t, true)

	var out settings.ListModelsOutput
	env.CallOK("list_llm_models", map[string]any{"provider": "openai"}, &out)
	require.Len(t, out.Providers, 1)
	assert.Equal(t, "api", out.Providers[0].Source)
	assert.Equal(t, []settings.ModelInfo{{ID: "gpt-live"}}, out.Providers[0].Models)

	env.CallOK("list_llm_models", map[string]any{}, &out)
	require.Len(t, out.Providers, len(llm.Providers()), "every provider when none is named")
	by := map[string]settings.ProviderModels{}
	for _, p := range out.Providers {
		by[p.Provider] = p
	}
	assert.Equal(t, "api", by["openai"].Source)
	assert.EqualValues(t, 1, asks.Load(), "the list is kept between the calls")
	assert.Equal(t, "default", by["anthropic"].Source, "no key: the candidates of mam")
	assert.NotEmpty(t, by["anthropic"].Reason)
	assert.Equal(t, llm.CandidateModels(llm.ProviderAnthropic), ids(by["anthropic"]))
	assert.Equal(t, "default", by["chatgpt-subscription"].Source, "not signed in")

	assert.NotContains(t, mcptest.Text(env.Call("list_llm_models", map[string]any{})), apiKey)
}

func TestListLLMModels_RefusesAnUnknownProviderAndIsUnavailableWithoutTheService(t *testing.T) {
	env, _ := withModels(t, false)
	assert.Contains(t, env.CallError("list_llm_models", map[string]any{"provider": "nobody"}), "validation:")

	bare := newEnv(t, mcpkit.Settings{})
	assert.Contains(t, bare.CallError("list_llm_models", map[string]any{}), "unavailable:")
}

func TestSetLLMAssignment_WarnsOfAModelThatIsInNoListButSavesIt(t *testing.T) {
	env, _ := withModels(t, true)
	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)
	name := ov.LLM.Agents[0].Agent
	set := func(primary, fallback string) settings.SetLLMOutput {
		var out settings.SetLLMOutput
		env.CallOK("set_llm_assignment", map[string]any{"routes": []map[string]any{{
			"agent":    name,
			"primary":  map[string]any{"provider": "openai", "model": primary},
			"fallback": map[string]any{"provider": "anthropic", "model": fallback},
		}}}, &out)
		return out
	}

	ok := set("gpt-live", llm.CandidateModels(llm.ProviderAnthropic)[0])
	assert.Empty(t, ok.Warnings, "both are in their lists")

	bad := set("  gpt-typo ", "claude-typo")
	assert.Equal(t, "gpt-typo", agent(t, bad.LLMSection, name).Primary.Model, "saved all the same")
	require.Len(t, bad.Warnings, 2)
	assert.Equal(t, settings.ModelWarning{Kind: "unlisted_model", Agent: name, Role: "primary", Provider: "openai", Model: "gpt-typo", Message: bad.Warnings[0].Message}, bad.Warnings[0])
	assert.Contains(t, bad.Warnings[0].Message, "list_llm_models")
	assert.Equal(t, "fallback", bad.Warnings[1].Role)
	assert.Contains(t, bad.Warnings[1].Message, "既定候補", "anthropic has no key: the warning says the list is mam's")
	assert.NotContains(t, bad.Warnings[0].Message+bad.Warnings[1].Message, apiKey)
}

func ids(p settings.ProviderModels) []string {
	var out []string
	for _, m := range p.Models {
		out = append(out, m.ID)
	}
	return out
}
