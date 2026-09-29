package llm_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// scripted is a Backend that answers from a script: each call takes the next result
// (the last one repeats).
type scripted struct {
	mu      sync.Mutex
	results []error // nil = success
	calls   int
	models  []string
}

func (s *scripted) Complete(_ context.Context, model string, _ llm.Request) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models = append(s.models, model)
	i := min(s.calls, len(s.results)-1)
	s.calls++
	if err := s.results[i]; err != nil {
		return "", err
	}
	return "ok from " + model, nil
}

func (s *scripted) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func status(p llm.Provider, code int) error {
	return &llm.APIError{Provider: p, Status: code, Message: "boom"}
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fixture builds a Client for AgentProducer (primary: openai, fallback: anthropic under the defaults).
type fixture struct {
	client   *llm.Client
	primary  *scripted
	fallback *scripted
	waits    []time.Duration
	clock    *clock
}

func newFixture(t *testing.T, primary, fallback []error) *fixture {
	t.Helper()
	cfg := llm.DefaultConfig()
	f := &fixture{
		primary:  &scripted{results: primary},
		fallback: &scripted{results: fallback},
		clock:    &clock{t: time.Unix(1_000_000, 0)},
	}
	f.client = llm.NewClient(cfg,
		map[llm.Provider]llm.Backend{llm.ProviderOpenAI: f.primary, llm.ProviderAnthropic: f.fallback, llm.ProviderGoogle: &scripted{results: []error{nil}}},
		llm.WithClock(f.clock.now),
		llm.WithSleep(func(_ context.Context, d time.Duration) error { f.waits = append(f.waits, d); return nil }),
	)
	return f
}

func TestClient_ResolvesProvidersFromConfig(t *testing.T) {
	cfg := llm.DefaultConfig()
	cfg.Routes[llm.AgentProducer] = llm.Route{
		Primary:  llm.Target{Provider: llm.ProviderGoogle, Model: "custom-model"},
		Fallback: llm.Target{Provider: llm.ProviderAnthropic, Model: "fb-model"},
	}
	google := &scripted{results: []error{nil}}
	client := llm.NewClient(cfg, map[llm.Provider]llm.Backend{
		llm.ProviderGoogle: google, llm.ProviderAnthropic: &scripted{results: []error{nil}}, llm.ProviderOpenAI: &scripted{results: []error{nil}},
	})

	res, err := client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.Equal(t, llm.ProviderGoogle, res.Provider)
	assert.Equal(t, "custom-model", res.Model)
	assert.False(t, res.Fallback)
	assert.Equal(t, []string{"custom-model"}, google.models)
}

func TestClient_UnknownAgentIsAnError(t *testing.T) {
	f := newFixture(t, []error{nil}, []error{nil})
	_, err := f.client.Complete(context.Background(), llm.Agent("nobody"), llm.Request{})
	require.Error(t, err)
	assert.Equal(t, 0, f.primary.callCount())
}

func TestClient_RetriesTimeoutsAnd429WithExponentialBackoff(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 429), &llm.APIError{Provider: llm.ProviderOpenAI, Err: context.DeadlineExceeded}, nil}, []error{nil})

	res, err := f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.False(t, res.Fallback, "the primary answered on its third attempt")
	assert.Equal(t, 3, f.primary.callCount())
	assert.Equal(t, 0, f.fallback.callCount())
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second}, f.waits, "the wait doubles")
}

func TestClient_BackoffHonoursRetryAfterAndIsCapped(t *testing.T) {
	limited := &llm.APIError{Provider: llm.ProviderOpenAI, Status: 429, RetryAfter: 5 * time.Second}
	tooLong := &llm.APIError{Provider: llm.ProviderOpenAI, Status: 429, RetryAfter: time.Hour}
	f := newFixture(t, []error{limited, tooLong, nil}, []error{nil})

	_, err := f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{5 * time.Second, 16 * time.Second}, f.waits)
}

func TestClient_RoutesToAlternativeWhenPrimaryKeepsFailing(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 503)}, []error{nil})

	res, err := f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Equal(t, llm.ProviderAnthropic, res.Provider)
	assert.Equal(t, 3, f.primary.callCount(), "MaxAttempts calls on the primary first")
}

func TestClient_DoesNotRetryRequestErrorsButStillTriesTheAlternative(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, http.StatusUnauthorized)}, []error{nil})

	res, err := f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Equal(t, 1, f.primary.callCount(), "a 401 is not retried")
	assert.Empty(t, f.waits)
}

func TestClient_FailsWhenEveryProviderFails(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 500)}, []error{status(llm.ProviderAnthropic, 500)})

	_, err := f.client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.Error(t, err)
	var api *llm.APIError
	assert.ErrorAs(t, err, &api)
	assert.Equal(t, 3, f.fallback.callCount())
}

func TestClient_CircuitBreakerOpensAndRoutesToAlternativeWithoutCallingPrimary(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 500)}, []error{nil})
	ctx := context.Background()

	// Threshold 5: the first call makes 3 failed attempts, the second call the 2 that open the breaker.
	for range 2 {
		_, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
		require.NoError(t, err)
	}
	require.Equal(t, 5, f.primary.callCount())

	res, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Equal(t, 5, f.primary.callCount(), "an open breaker keeps the primary from being called")
}

func TestClient_BreakerProbesAfterOpenPeriodAndClosesOnSuccess(t *testing.T) {
	// The primary fails five times, then recovers.
	primary := []error{
		status(llm.ProviderOpenAI, 500), status(llm.ProviderOpenAI, 500), status(llm.ProviderOpenAI, 500),
		status(llm.ProviderOpenAI, 500), status(llm.ProviderOpenAI, 500), nil,
	}
	f := newFixture(t, primary, []error{nil})
	ctx := context.Background()
	for range 2 {
		_, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
		require.NoError(t, err)
	}
	require.Equal(t, 5, f.primary.callCount())

	f.clock.advance(31 * time.Second) // OpenSeconds is 30
	res, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.False(t, res.Fallback, "the probe reaches the recovered primary")
	assert.Equal(t, 6, f.primary.callCount())

	res, err = f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.False(t, res.Fallback, "the breaker is closed again")
}

func TestClient_FailedProbeReopensBreakerWithoutRetrying(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 500)}, []error{nil})
	ctx := context.Background()
	for range 2 {
		_, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
		require.NoError(t, err)
	}
	f.clock.advance(31 * time.Second)

	res, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Equal(t, 6, f.primary.callCount(), "one probe call, no retries")

	_, err = f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.Equal(t, 6, f.primary.callCount(), "the breaker is open again")
}

func TestClient_BreakerIsPerProvider(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 500)}, []error{nil})
	ctx := context.Background()
	for range 3 {
		_, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
		require.NoError(t, err)
	}
	// OpenAI is open. The Director's primary is Anthropic and is unaffected.
	res, err := f.client.Complete(ctx, llm.AgentDirector, llm.Request{})
	require.NoError(t, err)
	assert.Equal(t, llm.ProviderAnthropic, res.Provider)
	assert.False(t, res.Fallback)
}

func TestClient_RequestErrorsDoNotOpenTheBreaker(t *testing.T) {
	f := newFixture(t, []error{llm.ErrNoAPIKey}, []error{nil})
	ctx := context.Background()
	for range 10 {
		_, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
		require.NoError(t, err)
	}
	assert.Equal(t, 10, f.primary.callCount(), "a missing key never trips the breaker")
}

func TestClient_StopsWhenCallerCancels(t *testing.T) {
	f := newFixture(t, []error{status(llm.ProviderOpenAI, 500)}, []error{nil})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.client.Complete(ctx, llm.AgentProducer, llm.Request{})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, f.fallback.callCount(), "no alternative is tried for a canceled caller")
}

func TestClient_PerCallTimeoutIsRetriedAsTimeout(t *testing.T) {
	cfg := llm.DefaultConfig()
	cfg.TimeoutSeconds = 1
	cfg.Retry = llm.RetryConfig{MaxAttempts: 2, BaseDelayMillis: 1, MaxDelayMillis: 1}
	slow := backendFunc(func(ctx context.Context, _ string, _ llm.Request) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	fast := &scripted{results: []error{nil}}
	client := llm.NewClient(cfg, map[llm.Provider]llm.Backend{llm.ProviderOpenAI: slow, llm.ProviderAnthropic: fast, llm.ProviderGoogle: fast})

	start := time.Now()
	res, err := client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.True(t, res.Fallback, "two timed-out attempts route to the alternative")
	assert.GreaterOrEqual(t, time.Since(start), 2*time.Second)
}

type backendFunc func(ctx context.Context, model string, req llm.Request) (string, error)

func (f backendFunc) Complete(ctx context.Context, model string, req llm.Request) (string, error) {
	return f(ctx, model, req)
}

func TestClient_MissingBackendIsSkipped(t *testing.T) {
	cfg := llm.DefaultConfig()
	fb := &scripted{results: []error{nil}}
	client := llm.NewClient(cfg, map[llm.Provider]llm.Backend{llm.ProviderAnthropic: fb})

	res, err := client.Complete(context.Background(), llm.AgentProducer, llm.Request{})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Equal(t, llm.ProviderAnthropic, res.Provider)
}
