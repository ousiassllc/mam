package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
)

// Request is one model call. The prompt is text; Agents that judge pictures (AI Video Judge)
// attach them as Images.
type Request struct {
	System string
	User   string
	// Images are shown to the model after User, in order (multimodal Agents only).
	Images []Image
	// MaxTokens bounds the answer. Zero selects DefaultMaxTokens.
	MaxTokens int
	// JSON asks the provider for a JSON object answer where it has such a mode.
	JSON bool
}

// Image is a picture attached to a Request. Caption, when set, is given to the model as text
// just before the picture (e.g. "frame 3, t=1.5s").
type Image struct {
	MimeType string
	Data     []byte
	Caption  string
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
	// Usage is what the answering call used, as the provider reported it; nil when the provider did not say (a Response with
	// no usage is "unknown", never zero tokens). The tokens of attempts that failed are not in it.
	Usage *Usage
}

// Completer is what an Agent needs from the adapter. Client implements it.
type Completer interface {
	Complete(ctx context.Context, agent Agent, req Request) (Response, error)
}

// Completion is what a Backend returns for one call: the text of the answer and the tokens the provider says it used (nil when the
// provider does not report them).
type Completion struct {
	Text  string
	Usage *Usage
}

// Backend calls one provider's API. It returns the answer. Errors that come from
// the provider or the network MUST be *APIError so the Client can classify them.
type Backend interface {
	Complete(ctx context.Context, model string, req Request) (Completion, error)
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
	// Permanent marks a failure that waiting does not cure (a plan's usage limit, a sign-in that must be
	// repeated): it is neither retried nor counted by the circuit breaker, and the call goes straight to the
	// alternative provider.
	Permanent bool
	// Notice is a sentence for the user about the cause of this failure (a usage limit, a sign-in to repeat). It
	// is told when the call is routed to the alternative provider (WithFallbackNotice) and is the reason shown when
	// every provider failed. It names no token.
	Notice string
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
		return !api.Permanent && (api.Status == 0 || api.Status == 429 || api.Status >= 500)
	}
	return false
}

// Client is the Reasoning LLM Adapter. It is safe for concurrent use.
//
// The routes can be replaced while it runs (Apply); the rest of the Config (the base URLs the backends were built with, the
// timeout, the retry policy, the breakers) is fixed when the Client is built.
type Client struct {
	cfg      Config
	backends map[Provider]Backend
	breakers map[Provider]*resilience.Breaker
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
	logger   *slog.Logger

	mu      sync.RWMutex
	routes  map[Agent]Route
	restart []string // what of the last applied Config only a restart can take (Apply)
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
		routes:   maps.Clone(cfg.Routes),
	}
	for _, o := range opts {
		o(c)
	}
	for p := range cfg.Providers {
		c.breakers[p] = resilience.NewBreaker(cfg.Breaker.FailureThreshold, cfg.breakerOpen(), c.now)
	}
	return c
}

// Apply makes the routes of cfg the ones the next calls use (a call in flight finishes on the route it began with), so a
// saved "LLM の割り当て" is in effect without a restart. The other settings of cfg cannot change while the Client runs: Apply
// returns, in words for the user, those that differ from what the Client was built with (the base URL of a provider, the timeout,
// the retry policy, the circuit breakers) and that therefore wait for a restart. Nil when there are none. The same list is told by
// the failures of Complete (RouteError.RestartPending) until the next Apply.
func (c *Client) Apply(cfg Config) (restart []string) {
	was, now := maps.Clone(c.cfg.Providers), maps.Clone(cfg.Providers)
	delete(was, ProviderLocal) // the address of the local server is read at every call (localllm.Add), so it never waits for a restart
	delete(now, ProviderLocal)
	if !maps.Equal(now, was) {
		restart = append(restart, "プロバイダーの base_url")
	}
	if cfg.TimeoutSeconds != c.cfg.TimeoutSeconds || cfg.Retry != c.cfg.Retry || cfg.Breaker != c.cfg.Breaker {
		restart = append(restart, "タイムアウト・リトライ・サーキットブレーカー")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routes = maps.Clone(cfg.Routes)
	c.restart = restart
	return restart
}

// Routes is the assignment the calls use now (a copy).
func (c *Client) Routes() map[Agent]Route {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return maps.Clone(c.routes)
}

// PrimaryOf is the target the Agent's route calls first now (the zero Target when the Agent has no route): what an estimate
// names, read at the time it is made, so that it follows a saved assignment.
func (c *Client) PrimaryOf(agent Agent) Target {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.routes[agent].Primary
}

// routeFor resolves the providers of the Agent from the routes in effect, with what waits for a restart.
func (c *Client) routeFor(agent Agent) (Route, []string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.routes[agent]
	if !ok {
		return Route{}, nil, fmt.Errorf("llm: no route configured for agent %q", agent)
	}
	return r, c.restart, nil
}

// Complete calls the primary target of the Agent, retrying timeouts and 429 with exponential
// backoff. When the primary's circuit breaker is open, or the call still fails, it routes to the
// Agent's alternative target. It fails only when every target failed.
func (c *Client) Complete(ctx context.Context, agent Agent, req Request) (Response, error) {
	route, restart, err := c.routeFor(agent)
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
	var tried []TargetFailure
	for i, t := range targets {
		done, err := c.callTarget(ctx, agent, t, req)
		if err == nil {
			return Response{Text: done.Text, Provider: t.Provider, Model: t.Model, Fallback: i > 0, Usage: done.Usage}, nil
		}
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		failures = append(failures, fmt.Errorf("%s/%s: %w", t.Provider, t.Model, err))
		tried = append(tried, TargetFailure{Target: t, Err: err})
		if i+1 < len(targets) {
			c.logger.Warn("llm target failed, routing to the alternative provider",
				"agent", agent, "provider", t.Provider, "next_provider", targets[i+1].Provider, "err", err)
			notifyFallback(ctx, err)
		}
	}
	// Only a route whose providers were all skipped by their breakers is an ErrCircuitOpen: when the
	// alternative was tried and failed for its own reason, that reason is what the caller must see.
	return Response{}, &RouteError{
		Agent: agent, Tried: tried, RestartPending: restart,
		err: resilience.RouteFailure(failures, func(err error) bool { return errors.Is(err, ErrCircuitOpen) }),
	}
}

// callTarget makes the call on one target with retries. The breaker of the provider is
// consulted before every attempt: once it is open no further attempt is made.
func (c *Client) callTarget(ctx context.Context, agent Agent, t Target, req Request) (Completion, error) {
	b := c.breakers[t.Provider]
	backend := c.backends[t.Provider]
	if b == nil || backend == nil {
		return Completion{}, fmt.Errorf("llm: provider %q is not available", t.Provider)
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
	done, err := resilience.Do(ctx, b, policy, hooks, func(ctx context.Context) (Completion, error) {
		return c.attempt(ctx, backend, t, req)
	})
	if errors.Is(err, resilience.ErrOpen) {
		return Completion{}, ErrCircuitOpen
	}
	return done, err
}

func (c *Client) attempt(ctx context.Context, backend Backend, t Target, req Request) (Completion, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout())
	defer cancel()
	done, err := backend.Complete(callCtx, t.Model, req)
	if err != nil && ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		return Completion{}, fmt.Errorf("%w after %s: %w", ErrTimeout, c.cfg.Timeout(), err)
	}
	return done, err
}

func retryAfter(err error) time.Duration {
	var api *APIError
	if errors.As(err, &api) {
		return api.RetryAfter
	}
	return 0
}
