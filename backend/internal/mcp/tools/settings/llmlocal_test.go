package settings_test

import (
	"context"
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
	assert.Equal(t, "/model-manager/local-llm", p.SettingsRoute)
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

type managedStub struct{ info llmlocal.ManagedInfo }

func (managedStub) Endpoint(context.Context) (string, string, error) {
	return "http://127.0.0.1:9", "k", nil
}
func (managedStub) Stop(context.Context) error                  { return nil }
func (m managedStub) Info(context.Context) llmlocal.ManagedInfo { return m.info }

// use_managed_runtime makes the provider "local" go to mam's own llama.cpp: the tool says where that server stands (and its model id, which the assignment names).
func TestSetLocalLLMServer_UseManagedRuntimeNeedsTheManagedServerAndShowsItsState(t *testing.T) {
	env, _, _ := withLocal(t, false)
	assert.Contains(t, env.CallError("set_local_llm_server", map[string]any{"use_managed_runtime": true}), "validation:", "no managed llama.cpp in this app")

	managedEnv := func(info llmlocal.ManagedInfo) *mcptest.Env {
		keys := credentialtest.New()
		s, f := newFiles(t, false)
		s.LLMLocal = llmlocal.New(f.llm, false, keys, nil)
		s.LLMLocal.SetManaged(managedStub{info})
		s.LLMModels = llmmodels.New(llmmodels.Options{Keys: keys, LocalBaseURL: s.LLMLocal.BaseURL, LocalManaged: s.LLMLocal.ManagedModels})
		return newEnv(t, s)
	}

	var local settings.LocalServer
	managedEnv(llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Model: "gemma-4-12b-it", Reason: "llama.cpp が導入されていません"}).
		CallOK("set_local_llm_server", map[string]any{"use_managed_runtime": true}, &local)
	assert.True(t, local.UseManagedRuntime)
	require.NotNil(t, local.ManagedRuntime)
	assert.Equal(t, "not_installed", local.ManagedRuntime.State)
	assert.Equal(t, "gemma-4-12b-it", local.ManagedRuntime.Model)
	assert.Contains(t, local.ManagedRuntime.Reason, "導入")

	ready := managedEnv(llmlocal.ManagedInfo{State: llmlocal.ManagedStopped, Model: "gemma-4-12b-it"})
	ready.CallOK("set_local_llm_server", map[string]any{"use_managed_runtime": true}, &local)
	var list settings.ListModelsOutput
	ready.CallOK("list_llm_models", map[string]any{"provider": "local"}, &list)
	require.Len(t, list.Providers[0].Models, 1)
	assert.Equal(t, "gemma-4-12b-it", list.Providers[0].Models[0].ID, "the picker offers the model of the managed server, while it is stopped too")
}

// use_managed_ollama points the provider "local" at mam's Ollama (#1104): the tool reads and writes the same switches as the page of the model manager, `use` says which server it is,
// and the two switches are never both true (a save of one clears the other; both in one call is a validation error).
func TestSetLocalLLMServer_UseManagedOllamaIsExclusiveWithTheManagedRuntimeAndShowsTheUse(t *testing.T) {
	env, _, _ := withLocal(t, false)
	assert.Contains(t, env.CallError("set_local_llm_server", map[string]any{"use_managed_ollama": true}), "validation:", "no managed Ollama in this app")

	keys := credentialtest.New()
	s, f := newFiles(t, false)
	s.LLMLocal = llmlocal.New(f.llm, false, keys, nil)
	s.LLMLocal.SetManaged(managedStub{llmlocal.ManagedInfo{State: llmlocal.ManagedStopped, Model: "gemma-4-12b-it"}})
	s.LLMLocal.SetManagedOllama(managedStub{llmlocal.ManagedInfo{State: llmlocal.ManagedNotInstalled, Reason: "Ollama が導入されていません"}})
	both := newEnv(t, s)

	assert.Contains(t, both.CallError("set_local_llm_server", map[string]any{"use_managed_runtime": true, "use_managed_ollama": true}), "validation:", "a contradictory pair")
	assert.Equal(t, llmlocal.UseCustom, s.LLMLocal.View().Use, "nothing was saved")

	var local settings.LocalServer
	both.CallOK("set_local_llm_server", map[string]any{"use_managed_runtime": true}, &local)
	assert.Equal(t, "llama-cpp", local.Use)
	assert.True(t, local.UseManagedRuntime)
	assert.False(t, local.UseManagedOllama)

	local = settings.LocalServer{} // a field the answer leaves out must not keep the value of the last one
	both.CallOK("set_local_llm_server", map[string]any{"use_managed_ollama": true}, &local)
	assert.Equal(t, "ollama", local.Use)
	assert.False(t, local.UseManagedRuntime, "choosing Ollama clears llama.cpp")
	assert.True(t, local.UseManagedOllama)
	require.NotNil(t, local.ManagedRuntime, "the state of the server in use has the same shape")
	assert.Equal(t, "not_installed", local.ManagedRuntime.State)

	var ov settings.Overview
	both.CallOK("get_global_settings", map[string]any{}, &ov)
	assert.Equal(t, local, ov.LLM.LocalServer, "get_global_settings reads what the tool saved")

	both.CallOK("set_local_llm_server", map[string]any{"use_managed_ollama": false}, &local)
	assert.Equal(t, "custom", local.Use)
}

// test_local_llm_server is unchanged for the address, and says where the server stands while one of mam is in use (Ollama too).
func TestTestLocalLLMServer_WhileOllamaOfMamIsInUseItSaysWhereItStands(t *testing.T) {
	keys := credentialtest.New()
	s, f := newFiles(t, false)
	s.LLMLocal = llmlocal.New(f.llm, false, keys, nil)
	s.LLMLocal.SetManagedOllama(managedStub{llmlocal.ManagedInfo{State: llmlocal.ManagedStopped, Model: "gemma4:12b", ModelName: "Gemma 4 12B"}})
	env := newEnv(t, s)
	env.CallOK("set_local_llm_server", map[string]any{"use_managed_ollama": true}, new(settings.LocalServer))

	var out settings.TestLocalServerOutput
	env.CallOK("test_local_llm_server", map[string]any{"base_url": "http://127.0.0.1:1"}, &out)

	assert.True(t, out.OK)
	assert.Contains(t, out.Message, "Ollama を使います")
	assert.Equal(t, []string{"gemma4:12b"}, out.Models)
}
