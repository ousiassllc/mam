package llm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

func TestBackends_ReportTheTokensTheProviderSays(t *testing.T) {
	for name, c := range map[string]struct {
		provider llm.Provider
		body     string
		want     llm.Usage
	}{
		"anthropic: the cache tokens are input too": {llm.ProviderAnthropic,
			`{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":100,"output_tokens":30,"cache_creation_input_tokens":20,"cache_read_input_tokens":5}}`,
			llm.Usage{InputTokens: 125, OutputTokens: 30}},
		"openai: the completion tokens include the reasoning": {llm.ProviderOpenAI,
			`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":210,"completion_tokens":640,"total_tokens":850}}`,
			llm.Usage{InputTokens: 210, OutputTokens: 640}},
		"google: the thinking tokens are output": {llm.ProviderGoogle,
			`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":300,"candidatesTokenCount":40,"thoughtsTokenCount":500,"totalTokenCount":840}}`,
			llm.Usage{InputTokens: 300, OutputTokens: 540}},
	} {
		ps := newProviderServer(t, 200, c.body, nil)
		done, err := backendFor(t, c.provider, ps, true).Complete(context.Background(), "m", llm.Request{User: "u"})
		require.NoError(t, err, name)
		require.NotNil(t, done.Usage, name)
		assert.Equal(t, c.want, *done.Usage, name)
	}
}

func TestBackends_AnAnswerWithoutUsageIsUnknownForEveryProvider(t *testing.T) {
	for provider, body := range map[llm.Provider]string{
		llm.ProviderOpenAI: `{"choices":[{"message":{"content":"ok"}}]}`,
		llm.ProviderGoogle: `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`,
	} {
		ps := newProviderServer(t, 200, body, nil)
		done, err := backendFor(t, provider, ps, true).Complete(context.Background(), "m", llm.Request{User: "u"})
		require.NoError(t, err)
		assert.Nil(t, done.Usage, provider)
	}
}

func TestClient_ResponseCarriesTheUsageOfTheAnsweringCallAndUnknownIsNotZero(t *testing.T) {
	f := newFixture(t, []error{nil}, []error{nil})
	res, err := f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.Nil(t, res.Usage, "the backend did not report tokens: unknown")

	f.primary.usage = &llm.Usage{InputTokens: 1200, OutputTokens: 340}
	res, err = f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	require.NotNil(t, res.Usage)
	assert.Equal(t, llm.Usage{InputTokens: 1200, OutputTokens: 340}, *res.Usage)
}

func TestClient_UsageIsThatOfTheTargetThatAnswered(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 400)}, []error{nil})
	f.primary.usage = &llm.Usage{InputTokens: 9, OutputTokens: 9}
	f.fallback.usage = &llm.Usage{InputTokens: 7, OutputTokens: 3}
	res, err := f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	require.NotNil(t, res.Usage)
	assert.Equal(t, llm.Usage{InputTokens: 7, OutputTokens: 3}, *res.Usage)
}

func TestLoadConfig_ReadsTheOldGeminiIdAsTheIdOfTheAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"routes": {
		"writer": {"primary": {"provider": "google", "model": "gemini-3.1-pro"}, "fallback": {"provider": "openai", "model": "gpt-5"}},
		"director": {"primary": {"provider": "anthropic", "model": "claude-opus-4-5"}, "fallback": {"provider": "google", "model": "gemini-3.1-pro"}},
		"producer": {"primary": {"provider": "openai", "model": "gemini-3.1-pro"}, "fallback": {"provider": "anthropic", "model": "claude-opus-4-5"}}
	}}`), 0o600))

	cfg, err := llm.LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "gemini-3.1-pro-preview", cfg.Routes[llm.AgentWriter].Primary.Model, "the id a file saved before the rename names")
	assert.Equal(t, "gemini-3.1-pro-preview", cfg.Routes[llm.AgentDirector].Fallback.Model)
	assert.Equal(t, "gemini-3.1-pro", cfg.Routes[llm.AgentProducer].Primary.Model, "another provider's id is not renamed")
}

func TestDefaultConfig_EveryTargetOfTheDefaultRoutesHasAPrice(t *testing.T) {
	for agent, r := range llm.DefaultConfig().Routes {
		for role, target := range map[string]llm.Target{"primary": r.Primary, "fallback": r.Fallback} {
			_, ok := llm.PriceOf(target)
			assert.True(t, ok, "%s %s: %s / %s has no price, so an estimate would be no_price", agent, role, target.Provider, target.Model)
		}
	}
}
