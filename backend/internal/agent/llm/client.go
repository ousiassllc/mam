package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
)

// Request is one model call. The prompt is text only: Agents build it from structured state.
type Request struct {
	System string
	User   string
	// MaxTokens bounds the answer. Zero selects DefaultMaxTokens.
	MaxTokens int
	// JSON asks the provider for a JSON object answer where it has such a mode.
	JSON bool
}

// DefaultMaxTokens is the answer limit when Request.MaxTokens is zero.
const DefaultMaxTokens = 8192

// Response is the model's answer with the target that produced it.
type Response struct {
	Text     string
	Provider Provider
	Model    string
	// Fallback is true when the Agent's alternative target answered.
	Fallback bool
}

// Completer is what an Agent needs from the adapter. Client implements it.
type Completer interface {
	Complete(ctx context.Context, agent Agent, req Request) (Response, error)
}

// Backend calls one provider's API. It returns the text of the answer. Errors that come from
// the provider or the network MUST be *APIError so the Client can classify them.
type Backend interface {
	Complete(ctx context.Context, model string, req Request) (string, error)
}

// ErrCircuitOpen marks a provider that was skipped because its circuit breaker is open.
var ErrCircuitOpen = fmt.Errorf("llm: %w", resilience.ErrOpen)

// ErrNoAPIKey means no API key is stored for the provider.
var ErrNoAPIKey = errors.New("llm: API key is not configured")

// ErrTimeout means a provider call exceeded Config.TimeoutSeconds.
var ErrTimeout = errors.New("llm: provider call timed out")

// APIError is a failure of a provider call. Status is the HTTP status, 0 for a network failure.
type APIError struct {
	Provider   Provider
	Status     int
	Message    string
	RetryAfter time.Duration
	Err        error
}

func (e *APIError) Error() string {
	switch {
	case e.Status != 0:
		return fmt.Sprintf("llm: %s returned HTTP %d: %s", e.Provider, e.Status, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("llm: %s request failed: %v", e.Provider, e.Err)
	}
	return fmt.Sprintf("llm: %s failed: %s", e.Provider, e.Message)
}

// Unwrap returns the underlying network error.
func (e *APIError) Unwrap() error { return e.Err }

// unavailable reports whether err says the provider is temporarily unusable: a timeout, a
// network failure, 429 or a 5xx. Only those are retried and counted by the circuit breaker;
// a 4xx or a missing key is a problem of the request or the setup, not of the provider.
func unavailable(err error) bool {
	if errors.Is(err, ErrTimeout) {
		return true
	}
	var api *APIError
	if errors.As(err, &api) {
		return api.Status == 0 || api.Status == 429 || api.Status >= 500
	}
	return false
}

// Client is the Reasoning LLM Adapter. It is safe for concurrent use.
type Client struct {
	cfg      Config
	backends map[Provider]Backend
	breakers map[Provider]*resilience.Breaker
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
	logger   *slog.Logger
}

// Option customises a Client.
type Option func(*Client)

// WithClock replaces the time source (breaker timing).
func WithClock(now func() time.Time) Option { return func(c *Client) { c.now = now } }

// WithSleep replaces the backoff wait.
func WithSleep(sleep func(ctx context.Context, d time.Duration) error) Option {
	return func(c *Client) { c.sleep = sleep }
}

// WithLogger sets the logger of retries and fail-overs.
func WithLogger(l *slog.Logger) Option { return func(c *Client) { c.logger = l } }

// NewClient returns a Client that resolves routes from cfg and calls the given backends.
func NewClient(cfg Config, backends map[Provider]Backend, opts ...Option) *Client {
	c := &Client{
		cfg:      cfg,
		backends: backends,
		breakers: make(map[Provider]*resilience.Breaker),
		now:      time.Now,
		sleep:    resilience.SleepContext,
		logger:   slog.New(slog.DiscardHandler),
	}
	for _, o := range opts {
		o(c)
	}
	for p := range cfg.Providers {
		c.breakers[p] = resilience.NewBreaker(cfg.Breaker.FailureThreshold, cfg.breakerOpen(), c.now)
	}
	return c
}

// Complete calls the primary target of the Agent, retrying timeouts and 429 with exponential
// backoff. When the primary's circuit breaker is open, or the call still fails, it routes to the
// Agent's alternative target. It fails only when every target failed.
func (c *Client) Complete(ctx context.Context, agent Agent, req Request) (Response, error) {
	route, err := c.cfg.RouteFor(agent)
	if err != nil {
		return Response{}, err
	}
	targets := []Target{route.Primary}
	if route.Fallback != route.Primary {
		targets = append(targets, route.Fallback)
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = DefaultMaxTokens
	}

	var failures []error
	for i, t := range targets {
		text, err := c.callTarget(ctx, agent, t, req)
		if err == nil {
			return Response{Text: text, Provider: t.Provider, Model: t.Model, Fallback: i > 0}, nil
		}
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		failures = append(failures, fmt.Errorf("%s/%s: %w", t.Provider, t.Model, err))
		if i+1 < len(targets) {
			c.logger.Warn("llm target failed, routing to the alternative provider",
				"agent", agent, "provider", t.Provider, "next_provider", targets[i+1].Provider, "err", err)
		}
	}
	return Response{}, fmt.Errorf("llm: every provider failed for agent %s: %w", agent, errors.Join(failures...))
}

// callTarget makes the call on one target with retries. The breaker of the provider is
// consulted before every attempt: once it is open no further attempt is made.
func (c *Client) callTarget(ctx context.Context, agent Agent, t Target, req Request) (string, error) {
	b := c.breakers[t.Provider]
	backend := c.backends[t.Provider]
	if b == nil || backend == nil {
		return "", fmt.Errorf("llm: provider %q is not available", t.Provider)
	}
	policy := resilience.Policy{MaxAttempts: c.cfg.Retry.MaxAttempts, BaseDelay: c.cfg.baseDelay(), MaxDelay: c.cfg.maxDelay()}
	hooks := resilience.Hooks{
		Unavailable: unavailable,
		RetryAfter:  retryAfter,
		Sleep:       c.sleep,
		OnRetry: func(attempt int, wait time.Duration, err error) {
			c.logger.Warn("llm call failed, retrying", "agent", agent, "provider", t.Provider,
				"attempt", attempt, "wait", wait, "err", err)
		},
	}
	text, err := resilience.Do(ctx, b, policy, hooks, func(ctx context.Context) (string, error) {
		return c.attempt(ctx, backend, t, req)
	})
	if errors.Is(err, resilience.ErrOpen) {
		return "", ErrCircuitOpen
	}
	return text, err
}

func (c *Client) attempt(ctx context.Context, backend Backend, t Target, req Request) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout())
	defer cancel()
	text, err := backend.Complete(callCtx, t.Model, req)
	if err != nil && ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%w after %s: %w", ErrTimeout, c.cfg.Timeout(), err)
	}
	return text, err
}

func retryAfter(err error) time.Duration {
	var api *APIError
	if errors.As(err, &api) {
		return api.RetryAfter
	}
	return 0
}
