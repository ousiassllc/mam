package chatgptsub_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/chatgptsub"
)

type fixedBackend struct {
	mu    sync.Mutex
	calls int
	text  string
}

func (f *fixedBackend) Complete(context.Context, string, llm.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.text, nil
}

// subscriptionClient is a Client whose Writer route is the subscription with the API-key OpenAI as the alternative.
func subscriptionClient(t *testing.T, srv *responses, src chatgptsub.TokenSource, fallback llm.Backend) *llm.Client {
	t.Helper()
	cfg := llm.DefaultConfig()
	cfg.Routes[llm.AgentWriter] = llm.Route{
		Primary:  llm.Target{Provider: llm.ProviderChatGPTSubscription, Model: "chosen-model"},
		Fallback: llm.Target{Provider: llm.ProviderOpenAI, Model: "gpt-x"},
	}
	cfg.Providers[llm.ProviderChatGPTSubscription] = llm.ProviderConfig{BaseURL: srv.URL}
	require.NoError(t, cfg.Validate())
	backends := map[llm.Provider]llm.Backend{llm.ProviderOpenAI: fallback}
	chatgptsub.Add(backends, cfg, src, nil)
	require.Contains(t, backends, llm.ProviderChatGPTSubscription)
	return llm.NewClient(cfg, backends, llm.WithSleep(func(context.Context, time.Duration) error { return nil }))
}

func TestClient_AgentOnTheSubscriptionWorksAndTheUsageLimitFailsOverWithoutRetry(t *testing.T) {
	srv := newResponses(t, func(w http.ResponseWriter, _ int) {
		sse(w, `{"type":"response.output_text.delta","delta":"script from the plan"}`, `{"type":"response.completed"}`)
	})
	fb := &fixedBackend{text: "script from the api key"}
	c := subscriptionClient(t, srv, &tokens{}, fb)

	res, err := c.Complete(context.Background(), llm.AgentWriter, llm.Request{System: "writer", User: "write"})
	require.NoError(t, err)
	assert.Equal(t, "script from the plan", res.Text)
	assert.Equal(t, llm.ProviderChatGPTSubscription, res.Provider)
	assert.Equal(t, "chosen-model", res.Model)
	assert.False(t, res.Fallback)
	assert.Zero(t, fb.calls)
	assert.Equal(t, "chosen-model", srv.reqs[0]["model"])

	limit := newResponses(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}`))
	})
	c = subscriptionClient(t, limit, &tokens{}, fb)
	var notices []string
	ctx := llm.WithFallbackNotice(context.Background(), func(m string) { notices = append(notices, m) })
	res, err = c.Complete(ctx, llm.AgentWriter, llm.Request{User: "write"})
	require.NoError(t, err)
	assert.Equal(t, "script from the api key", res.Text)
	assert.True(t, res.Fallback)
	assert.Equal(t, llm.ProviderOpenAI, res.Provider)
	assert.EqualValues(t, 1, limit.n.Load(), "a usage limit does not come back by waiting: one request, no retries")
	require.Len(t, notices, 1)
	assert.Contains(t, notices[0], "https://chatgpt.com/settings/usage")
	assert.Contains(t, notices[0], "代替のプロバイダー")
}

func TestClient_TheUsageLimitDoesNotOpenTheBreakerButOutagesDo(t *testing.T) {
	fb := &fixedBackend{text: "fallback"}
	limit := newResponses(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}`))
	})
	c := subscriptionClient(t, limit, &tokens{}, fb)
	for i := 0; i < 8; i++ { // more than the breaker threshold
		_, err := c.Complete(context.Background(), llm.AgentWriter, llm.Request{User: "x"})
		require.NoError(t, err)
	}
	assert.EqualValues(t, 8, limit.n.Load(), "every call still tries the subscription first: the limit is not counted as an outage")

	down := newResponses(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(503) })
	c = subscriptionClient(t, down, &tokens{}, fb)
	res, err := c.Complete(context.Background(), llm.AgentWriter, llm.Request{User: "x"})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.EqualValues(t, 3, down.n.Load(), "a 5xx is retried with backoff (3 attempts) before the fail-over")
}

func TestClient_NotSignedInRoutesToTheAlternativeWithTheReason(t *testing.T) {
	srv := newResponses(t, func(http.ResponseWriter, int) { t.Error("no request expected") })
	fb := &fixedBackend{text: "fallback"}
	c := subscriptionClient(t, srv, &tokens{err: errNotSignedIn()}, fb)
	var notice string
	ctx := llm.WithFallbackNotice(context.Background(), func(m string) { notice = m })
	res, err := c.Complete(ctx, llm.AgentWriter, llm.Request{User: "x"})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Contains(t, notice, "ログイン")
}
