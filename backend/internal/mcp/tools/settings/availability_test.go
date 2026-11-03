package settings_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

func providerState(ov settings.Overview, provider string) settings.ProviderState {
	for _, s := range ov.LLM.ProviderStates {
		if s.Provider == provider {
			return s
		}
	}
	return settings.ProviderState{}
}

// #1013: get_global_settings says which providers can be used (a saved key, a sign-in), and warns for a saved route that points at one that cannot.
func TestGetGlobalSettings_SaysWhichLLMProvidersCanBeUsedAndWarnsOfRoutesAtOthers(t *testing.T) {
	env, _ := withModels(t, true) // an OpenAI key is saved; the others have none
	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)

	require.Len(t, ov.LLM.ProviderStates, len(ov.LLM.Providers), "one state for each provider")
	openai := providerState(ov, "openai")
	assert.True(t, openai.Available)
	assert.Empty(t, openai.UnavailableMessage)
	anthropic := providerState(ov, "anthropic")
	assert.False(t, anthropic.Available)
	assert.Contains(t, anthropic.UnavailableMessage, "API キー")
	assert.Equal(t, "/settings/api-keys", anthropic.SettingsRoute)
	assert.Contains(t, providerState(ov, "chatgpt-subscription").UnavailableMessage, "ログイン")

	require.NotEmpty(t, ov.LLM.RouteWarnings, "the shipped defaults point at the providers without a key")
	for _, w := range ov.LLM.RouteWarnings {
		assert.NotEqual(t, "openai", w.Provider, "the provider with a key is no warning")
		assert.Contains(t, []string{"primary", "fallback"}, w.Role)
		assert.NotEmpty(t, w.Message)
		assert.Equal(t, "/settings/api-keys", w.SettingsRoute)
	}

	// Pointing the first agent's two routes at the provider with a key removes its warnings.
	name := ov.LLM.Agents[0].Agent
	before := 0
	for _, w := range ov.LLM.RouteWarnings {
		if w.Agent == name {
			before++
		}
	}
	require.Positive(t, before)
	var set settings.SetLLMOutput
	env.CallOK("set_llm_assignment", map[string]any{"routes": []map[string]any{{
		"agent": name, "primary": map[string]any{"provider": "openai", "model": "gpt-live"}, "fallback": map[string]any{"provider": "openai", "model": "gpt-live"},
	}}}, &set)
	for _, w := range set.RouteWarnings {
		assert.NotEqual(t, name, w.Agent, "saved at a provider with a key: no warning for this agent")
	}
	assert.Equal(t, len(ov.LLM.RouteWarnings)-before, len(set.RouteWarnings), "the save answer carries the warnings of the section too")
}

func TestListLLMModels_SaysWhetherEachProviderCanBeUsedAndCanLeaveOutTheOthers(t *testing.T) {
	env, _ := withModels(t, true)
	var all settings.ListModelsOutput
	env.CallOK("list_llm_models", map[string]any{}, &all)
	by := map[string]settings.ProviderModels{}
	for _, p := range all.Providers {
		by[p.Provider] = p
	}
	assert.True(t, by["openai"].Available)
	assert.False(t, by["anthropic"].Available)
	assert.Equal(t, "/settings/api-keys", by["anthropic"].SettingsRoute)
	assert.NotEmpty(t, by["anthropic"].UnavailableMessage)

	var only settings.ListModelsOutput
	env.CallOK("list_llm_models", map[string]any{"available_only": true}, &only)
	require.Len(t, only.Providers, 1, "only the provider with a key")
	assert.Equal(t, "openai", only.Providers[0].Provider)
}

// The image / video choices of the assignment say whether each model can be used now, and the saved routes at unusable models are listed.
func TestGetGlobalSettings_SaysWhetherEachImageAndVideoChoiceCanBeUsed(t *testing.T) {
	s, _ := newFiles(t, false)
	s.Models.SetAvailability(func(id string) (modelfilter.Availability, bool) {
		if id == "flux-2" {
			return modelfilter.Unusable(modelfilter.ReasonNoKey, "API キーが登録されていません（設定の「API キー」）", "/settings/api-keys"), true
		}
		return modelfilter.Usable(), true
	})
	env := newEnv(t, s)
	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)

	var flux, other *settings.ImageOption
	for i, o := range ov.Models.ImageModels {
		switch o.Model {
		case "flux-2":
			flux = &ov.Models.ImageModels[i]
		case "nano-banana":
			other = &ov.Models.ImageModels[i]
		}
	}
	require.NotNil(t, flux)
	require.NotNil(t, other)
	assert.False(t, flux.Available, "the choice stays in the list (the assignment is not hidden); it says it cannot be used")
	assert.Contains(t, flux.UnavailableMessage, "API キー")
	assert.Equal(t, "/settings/api-keys", flux.SettingsRoute)
	assert.True(t, other.Available)

	routed := false
	for _, u := range ov.Models.Unusable {
		if u.Model == "flux-2" {
			routed = true
			assert.Equal(t, "image", u.Media)
			assert.Contains(t, []string{"primary", "fallback"}, u.Role)
			assert.Equal(t, "/settings/api-keys", u.SettingsRoute)
		}
		assert.Equal(t, "flux-2", u.Model, "only that model cannot be used")
	}
	assert.True(t, routed, "the shipped routes name flux-2")
}
