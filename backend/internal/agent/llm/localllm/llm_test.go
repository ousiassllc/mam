package localllm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

func TestConfig_ValidatesTheLocalBaseURL(t *testing.T) {
	cfg := llm.DefaultConfig()
	require.NoError(t, cfg.Validate())
	assert.Equal(t, llm.LocalDefaultBaseURL, cfg.Providers[llm.ProviderLocal].BaseURL)

	cfg.Providers[llm.ProviderLocal] = llm.ProviderConfig{BaseURL: "http://169.254.169.254/latest"}
	assert.ErrorContains(t, cfg.Validate(), "provider local: base_url")

	// The cloud providers keep their own rules: http is not refused for them here (they are the operator's gateways), as before.
	cfg = llm.DefaultConfig()
	cfg.Providers[llm.ProviderOpenAI] = llm.ProviderConfig{BaseURL: "http://gateway.internal:8080"}
	assert.NoError(t, cfg.Validate())
}

func TestClientApply_TheLocalBaseURLChangesWithoutARestart(t *testing.T) {
	a := newLocalServer(t, func(int, map[string]any) (int, string) { return 200, chatAnswer("from a") })
	b := newLocalServer(t, func(int, map[string]any) (int, string) { return 200, chatAnswer("from b") })
	cfg := llm.DefaultConfig()
	cfg.Routes[llm.AgentWriter] = llm.Route{Primary: llm.Target{Provider: llm.ProviderLocal, Model: "m"}, Fallback: llm.Target{Provider: llm.ProviderLocal, Model: "m"}}
	// The address is read from the saved settings at every call (the app gives llmlocal.Service.BaseURL), so what Apply is handed does not matter for it.
	address := a.URL
	backends := llm.NewBackends(cfg, credentialtest.New(), nil)
	localllm.Add(backends, credentialtest.New(), nil, func() string { return address })
	client := llm.NewClient(cfg, backends)

	res, err := client.Complete(context.Background(), llm.AgentWriter, llm.Request{User: "u"})
	require.NoError(t, err)
	assert.Equal(t, "from a", res.Text)

	address = b.URL
	next := cfg
	next.Providers = map[llm.Provider]llm.ProviderConfig{}
	for p, pc := range cfg.Providers {
		next.Providers[p] = pc
	}
	next.Providers[llm.ProviderLocal] = llm.ProviderConfig{BaseURL: b.URL}
	assert.Empty(t, client.Apply(next), "the local base URL is not one of the settings that wait for a restart")

	res, err = client.Complete(context.Background(), llm.AgentWriter, llm.Request{User: "u"})
	require.NoError(t, err)
	assert.Equal(t, "from b", res.Text)

	next.Providers[llm.ProviderOpenAI] = llm.ProviderConfig{BaseURL: "http://gateway.internal"}
	assert.Equal(t, []string{"プロバイダーの base_url"}, client.Apply(next), "another provider's base_url still does")
}

// jsonScript answers each call with the next text; it records the prompts.
type jsonScript struct {
	provider llm.Provider
	texts    []string
	users    []string
}

func (s *jsonScript) Complete(_ context.Context, _ llm.Agent, req llm.Request) (llm.Response, error) {
	s.users = append(s.users, req.User)
	n := len(s.users) - 1
	text := s.texts[min(n, len(s.texts)-1)]
	return llm.Response{Text: text, Provider: s.provider, Model: "m", Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}}, nil
}

type obj struct {
	Title string `json:"title"`
}

func TestCompleteJSON_ALocalModelIsAskedAgainWithTheReason(t *testing.T) {
	c := &jsonScript{provider: llm.ProviderLocal, texts: []string{"Sure! Here you go", `{"title":`, `{"title":"x"}`}}

	got, res, err := llm.CompleteJSONResponse[obj](context.Background(), c, llm.AgentWriter, llm.Request{User: "write"})

	require.NoError(t, err)
	assert.Equal(t, "x", got.Title)
	require.Len(t, c.users, 3)
	assert.Equal(t, "write", c.users[0])
	assert.Contains(t, c.users[1], "write")
	assert.Contains(t, c.users[1], "no JSON object")
	assert.Contains(t, c.users[2], "cut off")
	assert.Equal(t, &llm.Usage{InputTokens: 30, OutputTokens: 15}, res.Usage, "the tokens of every call are reported")
}

func TestCompleteJSON_ALocalModelThatNeverAnswersJSONFailsWithTheReason(t *testing.T) {
	c := &jsonScript{provider: llm.ProviderLocal, texts: []string{"I cannot do that."}}

	_, res, err := llm.CompleteJSONResponse[obj](context.Background(), c, llm.AgentWriter, llm.Request{User: "write"})

	require.ErrorIs(t, err, llm.ErrInvalidOutput)
	assert.Len(t, c.users, 1+llm.MaxJSONRetries, "asked again at most MaxJSONRetries times")
	assert.Contains(t, err.Error(), "local/m")
	assert.Contains(t, err.Error(), "3 calls")
	assert.Contains(t, err.Error(), "no JSON object")
	assert.Equal(t, 30, res.Usage.InputTokens)
}

func TestCompleteJSON_TheCloudProvidersAreNotAskedAgain(t *testing.T) {
	for _, p := range []llm.Provider{llm.ProviderAnthropic, llm.ProviderOpenAI, llm.ProviderGoogle, llm.ProviderChatGPTSubscription, llm.ProviderChatGPTCodex} {
		c := &jsonScript{provider: p, texts: []string{"not json"}}

		_, _, err := llm.CompleteJSONResponse[obj](context.Background(), c, llm.AgentWriter, llm.Request{User: "write"})

		require.ErrorIs(t, err, llm.ErrInvalidOutput, p)
		assert.Len(t, c.users, 1, "%s: one call, as before", p)
		assert.False(t, strings.Contains(err.Error(), "calls"), p)
	}
}

func TestCompleteJSON_AnAnswerThatDecodesIsNotAskedAgain(t *testing.T) {
	c := &jsonScript{provider: llm.ProviderLocal, texts: []string{"```json\n{\"title\":\"ok\"}\n```"}}

	got, err := llm.CompleteJSON[obj](context.Background(), c, llm.AgentWriter, llm.Request{User: "write"})

	require.NoError(t, err)
	assert.Equal(t, "ok", got.Title)
	assert.Len(t, c.users, 1)
}

func TestPriceOf_ALocalModelIsFreeWhateverItsName(t *testing.T) {
	p, ok := llm.PriceOf(llm.Target{Provider: llm.ProviderLocal, Model: "qwen3.8:27b"})

	assert.True(t, ok)
	assert.Zero(t, p.Cost(llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}))
	_, ok = llm.PriceOf(llm.Target{Provider: llm.ProviderOpenAI, Model: "no-such-model"})
	assert.False(t, ok, "the others keep their table")
}
