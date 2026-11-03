package settings_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// withLocal serves the tools over a fake OpenAI-compatible local server (models gemma-4-12b and qwen3.8:27b) and a key store of its own.
func withLocal(t *testing.T, pinned bool) (*mcptest.Env, *httptest.Server, *credentialtest.Memory) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gemma-4-12b"},{"id":"qwen3.8:27b"}]}`))
	}))
	t.Cleanup(srv.Close)
	keys := credentialtest.New()
	s, f := newFiles(t, pinned)
	s.LLMLocal = llmlocal.New(f.llm, pinned, keys, nil)
	s.LLMModels = llmmodels.New(llmmodels.Options{Keys: keys, LocalBaseURL: s.LLMLocal.BaseURL})
	return newEnv(t, s), srv, keys
}

func TestSetLocalLLMServer_SavesTheAddressAndTheKeyAndTheModelsAreListedAndAssignable(t *testing.T) {
	env, srv, keys := withLocal(t, false)

	var local settings.LocalServer
	env.CallOK("set_local_llm_server", map[string]any{"base_url": srv.URL + "/v1/", "api_key": "sk-local-secret", "unload_before_generation": true}, &local)
	assert.True(t, local.Available)
	assert.Equal(t, srv.URL, local.BaseURL)
	assert.True(t, local.KeyConfigured)
	assert.True(t, local.UnloadBeforeGeneration)
	got, err := keys.Get("local")
	require.NoError(t, err)
	assert.Equal(t, "sk-local-secret", got)

	var ov settings.Overview
	env.CallOK("get_global_settings", map[string]any{}, &ov)
	assert.Equal(t, local, ov.LLM.LocalServer)
	assert.Contains(t, ov.LLM.Providers, "local")
	states := map[string]settings.ProviderState{}
	for _, st := range ov.LLM.ProviderStates {
		states[st.Provider] = st
	}
	assert.True(t, states["local"].Available, "no read has failed: usable")

	var models settings.ListModelsOutput
	env.CallOK("list_llm_models", map[string]any{"provider": "local"}, &models)
	require.Len(t, models.Providers, 1)
	assert.Equal(t, "api", models.Providers[0].Source)
	assert.Equal(t, []settings.ModelInfo{{ID: "gemma-4-12b"}, {ID: "qwen3.8:27b"}}, models.Providers[0].Models)
	assert.True(t, models.Providers[0].Available)

	// Every Agent can be assigned to the local server as the primary and the fallback.
	var routes []map[string]any
	for _, a := range ov.LLM.Agents {
		routes = append(routes, map[string]any{
			"agent":    a.Agent,
			"primary":  map[string]any{"provider": "local", "model": "qwen3.8:27b"},
			"fallback": map[string]any{"provider": "local", "model": "gemma-4-12b"},
		})
	}
	var set settings.SetLLMOutput
	env.CallOK("set_llm_assignment", map[string]any{"routes": routes}, &set)
	assert.Empty(t, set.Warnings, "both models are in the server's list")
	assert.Empty(t, set.RouteWarnings)
	for _, a := range set.Agents {
		assert.Equal(t, settings.Target{Provider: "local", Model: "qwen3.8:27b"}, a.Primary, a.Agent)
		assert.Equal(t, settings.Target{Provider: "local", Model: "gemma-4-12b"}, a.Fallback, a.Agent)
	}

	assert.NotContains(t, mcptest.Text(env.Call("get_global_settings", map[string]any{})), "sk-local-secret")
	assert.NotContains(t, mcptest.Text(env.Call("set_local_llm_server", map[string]any{"base_url": srv.URL})), "sk-local-secret")
}

func TestSetLocalLLMServer_RefusesBadInput(t *testing.T) {
	env, _, _ := withLocal(t, false)

	assert.Contains(t, env.CallError("set_local_llm_server", map[string]any{"base_url": "http://169.254.169.254/"}), "validation:")
	assert.Contains(t, env.CallError("set_local_llm_server", map[string]any{"base_url": "localhost:11434"}), "validation:")
	assert.Contains(t, env.CallError("set_local_llm_server", map[string]any{}), "validation:", "nothing to change")
	assert.Contains(t, env.CallError("set_local_llm_server", map[string]any{"api_key": "has space"}), "validation:")

	bare := newEnv(t, mcpkitSettingsWithout())
	assert.Contains(t, bare.CallError("set_local_llm_server", map[string]any{"base_url": "http://localhost:1"}), "unavailable:")
	assert.Contains(t, bare.CallError("test_local_llm_server", map[string]any{}), "unavailable:")
}

func TestSetLocalLLMServer_APinnedFileIsAConflict(t *testing.T) {
	env, srv, _ := withLocal(t, true)

	assert.Contains(t, env.CallError("set_local_llm_server", map[string]any{"base_url": srv.URL}), "conflict:")
}

func TestTestLocalLLMServer_ListsTheModelsOrTellsWhyItCannotConnect(t *testing.T) {
	env, srv, _ := withLocal(t, false)

	var ok settings.TestLocalServerOutput
	env.CallOK("test_local_llm_server", map[string]any{"base_url": srv.URL}, &ok)
	assert.True(t, ok.OK)
	assert.Equal(t, 2, ok.ModelCount)
	assert.Equal(t, []string{"gemma-4-12b", "qwen3.8:27b"}, ok.Models)
	assert.Nil(t, ok.Failure)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := "http://" + l.Addr().String()
	require.NoError(t, l.Close())
	var down settings.TestLocalServerOutput
	env.CallOK("test_local_llm_server", map[string]any{"base_url": addr}, &down)
	assert.False(t, down.OK)
	assert.Contains(t, down.Message, "ローカルサーバーに接続できません")
	require.NotNil(t, down.Failure)
	assert.EqualValues(t, "local_llm_unreachable", down.Failure.Code)
	assert.Contains(t, down.Failure.Remedy, "ベース URL")
	assert.Empty(t, down.Models)

	assert.Contains(t, env.CallError("test_local_llm_server", map[string]any{"base_url": "http://[fe80::1]:11434"}), "validation:")
}

func TestListLLMModels_ADownLocalServerIsUnusableWithTheReason(t *testing.T) {
	env, _, _ := withLocal(t, false)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := "http://" + l.Addr().String()
	require.NoError(t, l.Close())
	env.CallOK("set_local_llm_server", map[string]any{"base_url": addr}, &settings.LocalServer{})

	var models settings.ListModelsOutput
	env.CallOK("list_llm_models", map[string]any{"provider": "local"}, &models)
	require.Len(t, models.Providers, 1)
	p := models.Providers[0]
	assert.Equal(t, "default", p.Source)
	assert.Equal(t, "ローカルサーバーに接続できません", p.Reason)
	assert.False(t, p.Available)
	assert.Equal(t, "/settings/llm", p.SettingsRoute)
	assert.Empty(t, p.Models)

	env.CallOK("list_llm_models", map[string]any{"available_only": true}, &models)
	for _, q := range models.Providers {
		assert.NotEqual(t, "local", q.Provider, "an unusable provider is left out when asked")
	}

	// Saving an assignment to the down server is allowed, with a warning that says why the model cannot be checked.
	var set settings.SetLLMOutput
	env.CallOK("set_llm_assignment", map[string]any{"routes": []map[string]any{{
		"agent":    "writer",
		"primary":  map[string]any{"provider": "local", "model": "gemma-4-12b"},
		"fallback": map[string]any{"provider": "local", "model": "gemma-4-12b"},
	}}}, &set)
	require.NotEmpty(t, set.Warnings)
	assert.Contains(t, set.Warnings[0].Message, "ローカルサーバーに接続できません")
	assert.NotEmpty(t, set.RouteWarnings, "the route points at a provider that cannot be used now")
}

func mcpkitSettingsWithout() mcpkit.Settings { return mcpkit.Settings{} }
