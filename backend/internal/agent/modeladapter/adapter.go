package modeladapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/limitclock"
	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ImageOutput is a generated image with how it was made.
type ImageOutput struct {
	ImageResult
	Model         domain.ModelName
	FellBack      bool // the alternative model made it
	EstimatedCost float64
}

// VideoOutput is a generated video with how it was made.
type VideoOutput struct {
	VideoResult
	Model           domain.ModelName
	Resolution      string
	DurationSeconds float64
	Request         VideoRequest // what was sent to the model (the images are omitted in records)
	FellBack        bool
	EstimatedCost   float64
}

// state is what a call runs with: the setting, the backends built from it, and the breaker and
// concurrency limit of every vendor. Reload swaps it whole; a call in flight keeps the one it started with.
type state struct {
	cfg      Config
	backends Backends
	vendors  map[Vendor]VendorConfig
	breakers map[Vendor]*resilience.Breaker
	limiters map[Vendor]*resilience.Limiter
}

// Adapter is the Model Adapter. It is safe for concurrent use.
type Adapter struct {
	state atomic.Pointer[state]
	// Gate refuses an adult model while the age confirmation is off: before any call of a route, and as unavailable.
	Gate  agegate.Gate
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
	// timeLimit maps the limit of a video job to the one that is applied; nil applies it as given.
	timeLimit func(time.Duration) time.Duration
	logger    *slog.Logger
}

// Option customises an Adapter.
type Option func(*Adapter)

// WithClock replaces the time source (breaker timing).
func WithClock(now func() time.Time) Option { return func(a *Adapter) { a.now = now } }

// WithSleep replaces the backoff wait.
func WithSleep(sleep func(ctx context.Context, d time.Duration) error) Option {
	return func(a *Adapter) { a.sleep = sleep }
}

// WithTimeLimit maps the time limit of every video job (VideoJob.MaxTime) through limit. A Policy
// cannot hold less than 10 s: a test shortens it here instead of waiting.
func WithTimeLimit(limit func(time.Duration) time.Duration) Option {
	return func(a *Adapter) { a.timeLimit = limit }
}

// WithLogger sets the logger of retries and fail-overs.
func WithLogger(l *slog.Logger) Option { return func(a *Adapter) { a.logger = l } }

// New returns an Adapter that resolves routes from cfg and calls the given backends.
func New(cfg Config, backends Backends, opts ...Option) *Adapter {
	a := &Adapter{now: time.Now, sleep: resilience.SleepContext, logger: slog.New(slog.DiscardHandler)}
	for _, o := range opts {
		o(a)
	}
	a.Reload(cfg, backends)
	return a
}

// Reload makes the Adapter run with a new setting and backends from now on (the endpoints and models the
// user saved take effect at once). Calls in flight finish with the old ones. A vendor that is in both
// keeps its circuit breaker, and its concurrency limit unless the configured maximum changed.
func (a *Adapter) Reload(cfg Config, backends Backends) {
	old := a.state.Load()
	st := &state{
		cfg: cfg, backends: backends, vendors: cfg.VendorSettings(),
		breakers: make(map[Vendor]*resilience.Breaker), limiters: make(map[Vendor]*resilience.Limiter),
	}
	for v, vc := range st.vendors {
		st.breakers[v] = resilience.NewBreaker(cfg.Breaker.FailureThreshold, time.Duration(cfg.Breaker.OpenSeconds)*time.Second, a.now)
		st.limiters[v] = resilience.NewLimiter(vc.MaxConcurrency)
		if old != nil && old.breakers[v] != nil {
			st.breakers[v] = old.breakers[v]
			if old.limiters[v].Max() == vc.MaxConcurrency {
				st.limiters[v] = old.limiters[v]
			}
		}
	}
	a.state.Store(st)
}

// Config returns the setting the Adapter runs with.
func (a *Adapter) Config() Config { return a.state.Load().cfg }

// Unavailable says why the image or video model cannot be called now (domain.ExcludedNoBackend: it has no
// backend or no API key is stored for its vendor; domain.ExcludedCircuitOpen: its vendor's circuit
// breaker is open), or "" when it can. It does not consume the breaker's probe call.
func (a *Adapter) Unavailable(m domain.ModelName) string {
	if a.Gate.Refuses(m) {
		return agegate.Excluded
	}
	st := a.state.Load()
	backend := any(st.backends.Video[m])
	if backend == nil {
		if backend = any(st.backends.Image[m]); backend == nil {
			return domain.ExcludedNoBackend
		}
	}
	// Keys are added at run time, so the stored key is looked up on every call.
	if c, ok := backend.(interface{ Configured() bool }); ok && !c.Configured() {
		return domain.ExcludedNoBackend
	}
	if b := st.breakers[VendorOf(m)]; b == nil || b.IsOpen() {
		return domain.ExcludedCircuitOpen
	}
	return ""
}

// Snapshot is the setting and the backends a call that starts now runs with (for the direct calls of package direct).
func (a *Adapter) Snapshot() (Config, Backends) {
	st := a.state.Load()
	return st.cfg, st.backends
}

// Call runs fn like the Adapter's own calls: behind the vendor's breaker, concurrency limit, retry and timeout, no fail-over.
func Call[T any](ctx context.Context, a *Adapter, v Vendor, clock *limitclock.Clock, model string, fn func(context.Context) (T, error)) (T, error) {
	return call(ctx, a, a.state.Load(), v, clock, fn, model)
}

// EstimateImage is the most one image of the purpose can cost: the dearer of the primary model and
// the alternative the call falls back to. It is what a budget check holds before the call.
func (a *Adapter) EstimateImage(purpose ImagePurpose) float64 {
	cfg := a.Config()
	route := cfg.ImageRouteFor(purpose)
	return max(cfg.ImageCost(route.Primary.Model), cfg.ImageCost(route.Fallback.Model))
}

// GenerateImage makes an image with the primary model of the purpose, and with the alternative
// when the primary is unavailable (its breaker is open, or it failed after the retries).
func (a *Adapter) GenerateImage(ctx context.Context, purpose ImagePurpose, req ImageRequest) (ImageOutput, error) {
	st := a.state.Load()
	route := st.cfg.ImageRouteFor(purpose)
	if err := a.Gate.Check(route.Primary.Model, route.Fallback.Model); err != nil {
		return ImageOutput{}, err
	}
	targets := []ImageTarget{route.Primary}
	if route.Fallback != route.Primary {
		targets = append(targets, route.Fallback)
	}
	if req.AspectRatio == "" {
		req.AspectRatio = domain.DefaultAspectRatio
	}
	var failures []error
	for i, t := range targets {
		backend := st.backends.Image[t.Model]
		if backend == nil {
			failures = append(failures, fmt.Errorf("%s: no backend", t.Model))
			continue
		}
		mc := st.cfg.ModelSettings(t.Model)
		apiModel := mc.Model
		if t.Fast {
			apiModel = mc.FastModel
		}
		treq := req
		if !st.cfg.AcceptsImages(t.Model) && len(req.Inputs) > 0 {
			a.logger.Warn("image model cannot use reference images, generating from the prompt only", "model", t.Model)
			treq.Inputs = nil
		}
		res, err := call(ctx, a, st, VendorOf(t.Model), nil, func(ctx context.Context) (ImageResult, error) {
			return backend.GenerateImage(ctx, apiModel, treq)
		}, string(t.Model))
		if err == nil {
			return ImageOutput{ImageResult: res, Model: t.Model, FellBack: i > 0, EstimatedCost: st.cfg.ImageCost(t.Model)}, nil
		}
		if ctx.Err() != nil {
			return ImageOutput{}, ctx.Err()
		}
		failures = append(failures, fmt.Errorf("%s: %w", t.Model, err))
		a.logFailover(t.Model, i, len(targets), err)
	}
	return ImageOutput{}, fmt.Errorf("modeladapter: every image model failed for %s: %w", purpose, routeFailure(failures))
}

// GenerateVideo makes one Take. It renders the job for the primary target and calls it; when the
// primary is unavailable (breaker open, or failed after the retries) it renders the job again for
// the alternative and calls that (F-14 "Different Model", model-adapters.md §7).
func (a *Adapter) GenerateVideo(ctx context.Context, primary, fallback VideoTarget, job VideoJob) (VideoOutput, error) {
	if err := a.Gate.Check(primary.Model, fallback.Model); err != nil {
		return VideoOutput{}, err
	}
	st := a.state.Load()
	if a.timeLimit != nil {
		job.MaxTime = a.timeLimit(job.MaxTime)
	}
	targets := []VideoTarget{primary}
	if fallback != primary {
		targets = append(targets, fallback)
	}
	var failures []error
	clock := &limitclock.Clock{Limit: job.MaxTime}
	for i, t := range targets {
		backend := st.backends.Video[t.Model]
		if backend == nil {
			failures = append(failures, fmt.Errorf("%s: no backend", t.Model))
			continue
		}
		req, err := st.cfg.Prepare(t, job)
		if err != nil {
			return VideoOutput{}, err
		}
		apiModel := st.cfg.ModelSettings(t.Model).Model
		res, err := call(ctx, a, st, VendorOf(t.Model), clock, func(ctx context.Context) (VideoResult, error) {
			return backend.GenerateVideo(ctx, apiModel, req)
		}, string(t.Model))
		if err == nil {
			return VideoOutput{
				VideoResult: res, Model: t.Model, Resolution: t.Resolution, DurationSeconds: req.DurationSeconds,
				Request: req, FellBack: i > 0, EstimatedCost: st.cfg.VideoCost(t, req.DurationSeconds),
			}, nil
		}
		if ctx.Err() != nil {
			return VideoOutput{}, ctx.Err()
		}
		if clock.Expired() {
			// The alternative has no time left either: say that, not what the cut-off call returned.
			return VideoOutput{}, fmt.Errorf("%w (%s): %w", ErrTimeLimit, job.MaxTime, err)
		}
		failures = append(failures, fmt.Errorf("%s: %w", t.Model, err))
		a.logFailover(t.Model, i, len(targets), err)
	}
	return VideoOutput{}, fmt.Errorf("modeladapter: every video model failed: %w", routeFailure(failures))
}

// call runs fn against one vendor: it skips the vendor while its breaker is open, takes a
// concurrency slot, and retries unavailable answers with exponential backoff. A 429 also lowers
// the vendor's concurrency limit. The call is bounded by the vendor's timeout, and by clock, which
// starts once the slot is held.
func call[T any](ctx context.Context, a *Adapter, st *state, v Vendor, clock *limitclock.Clock, fn func(context.Context) (T, error), model string) (T, error) {
	var zero T
	b, lim := st.breakers[v], st.limiters[v]
	if b == nil || lim == nil {
		return zero, fmt.Errorf("modeladapter: vendor %q is not configured", v)
	}
	if b.IsOpen() {
		return zero, fmt.Errorf("%s: %w", model, ErrCircuitOpen)
	}
	release, err := lim.Acquire(ctx)
	if err != nil {
		return zero, err
	}
	ok := false
	defer func() { release(ok) }()
	ctx, cancelLimit := clock.Start(ctx)
	defer cancelLimit()

	policy := resilience.Policy{
		MaxAttempts: st.cfg.Retry.MaxAttempts,
		BaseDelay:   time.Duration(st.cfg.Retry.BaseDelayMillis) * time.Millisecond,
		MaxDelay:    time.Duration(st.cfg.Retry.MaxDelayMillis) * time.Millisecond,
	}
	hooks := resilience.Hooks{
		Unavailable: IsUnavailable, Retryable: retryable, RetryAfter: retryAfter, Sleep: a.sleep,
		OnRetry: func(attempt int, wait time.Duration, err error) {
			if RateLimited(err) {
				lim.Throttle()
			}
			a.logger.Warn("model call failed, retrying", "model", model, "attempt", attempt, "wait", wait, "err", err)
		},
	}
	timeout := time.Duration(st.vendors[v].TimeoutSeconds) * time.Second
	out, err := resilience.Do(ctx, b, policy, hooks, func(ctx context.Context) (T, error) {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		out, err := fn(callCtx)
		if err != nil && ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return zero, fmt.Errorf("%w after %s: %w", ErrTimeout, timeout, err)
		}
		return out, err
	})
	if err != nil {
		if RateLimited(err) {
			lim.Throttle()
		}
		if errors.Is(err, resilience.ErrOpen) {
			return zero, fmt.Errorf("%s: %w", model, ErrCircuitOpen)
		}
		return zero, err
	}
	ok = true
	return out, nil
}

// ConcurrencyLimit returns the number of calls the vendor may have in flight right now (it
// follows the vendor's rate limit: halved on 429, raised again while calls succeed).
func (a *Adapter) ConcurrencyLimit(v Vendor) int {
	if l := a.state.Load().limiters[v]; l != nil {
		return l.Current()
	}
	return 0
}
