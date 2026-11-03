package llm_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// A saved "LLM の割り当て" is applied to the running client: the next call goes to the new route with no restart (#1006).
func TestClient_ApplyMakesTheNextCallUseTheNewRoute(t *testing.T) {
	cfg := llm.DefaultConfig()
	openai, google := &scripted{results: []error{nil}}, &scripted{results: []error{nil}}
	client := llm.NewClient(cfg, map[llm.Provider]llm.Backend{
		llm.ProviderOpenAI: openai, llm.ProviderGoogle: google, llm.ProviderAnthropic: &scripted{results: []error{nil}},
	})
	res, err := client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.Equal(t, llm.ProviderOpenAI, res.Provider, "the default route of the Producer starts on openai")

	next := llm.DefaultConfig()
	next.Routes[llm.AgentProducer] = llm.Route{
		Primary:  llm.Target{Provider: llm.ProviderGoogle, Model: "new-model"},
		Fallback: llm.Target{Provider: llm.ProviderAnthropic, Model: "fb"},
	}
	assert.Empty(t, client.Apply(next), "a route is applied at once: nothing waits for a restart")

	res, err = client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.Equal(t, llm.ProviderGoogle, res.Provider)
	assert.Equal(t, "new-model", res.Model)
	assert.Equal(t, 1, openai.callCount(), "the old route is not called again")
	assert.Equal(t, llm.Target{Provider: llm.ProviderGoogle, Model: "new-model"}, client.PrimaryOf(llm.AgentProducer))
}

// What the running client cannot change (the base URLs it built its backends with, the timeout, the retry policy, the
// breakers) is reported as waiting for a restart, and a failure of a call says so.
func TestClient_ApplyReportsWhatOnlyARestartApplies(t *testing.T) {
	missing := &scripted{results: []error{fmt.Errorf("%w: openai", llm.ErrNoAPIKey)}}
	client := llm.NewClient(llm.DefaultConfig(), map[llm.Provider]llm.Backend{
		llm.ProviderOpenAI: missing, llm.ProviderAnthropic: missing, llm.ProviderGoogle: missing,
	})
	next := llm.DefaultConfig()
	next.Providers[llm.ProviderOpenAI] = llm.ProviderConfig{BaseURL: "https://proxy.example"}
	next.TimeoutSeconds = 300

	restart := client.Apply(next)
	assert.Equal(t, []string{"プロバイダーの base_url", "タイムアウト・リトライ・サーキットブレーカー"}, restart)

	_, err := client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	var route *llm.RouteError
	require.ErrorAs(t, err, &route)
	assert.Equal(t, restart, route.RestartPending)

	assert.Empty(t, client.Apply(llm.DefaultConfig()), "what is applied again matches what the client was built with")
}
