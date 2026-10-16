package modeladapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

// Adapter is the Model Adapter. It is safe for concurrent use.
type Adapter struct {
	cfg      Config
	backends Backends
	breakers map[Vendor]*resilience.Breaker
	limiters map[Vendor]*limiter
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
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
	a := &Adapter{
		cfg: cfg, backends: backends,
		breakers: make(map[Vendor]*resilience.Breaker), limiters: make(map[Vendor]*limiter),
		now: time.Now, sleep: resilience.SleepContext, logger: slog.New(slog.DiscardHandler),
	}
	for _, o := range opts {
		o(a)
	}
	for v, vc := range cfg.Vendors {
		a.breakers[v] = resilience.NewBreaker(cfg.Breaker.FailureThreshold, time.Duration(cfg.Breaker.OpenSeconds)*time.Second, a.now)
		a.limiters[v] = newLimiter(vc.MaxConcurrency)
	}
	return a
}

// Config returns the setting the Adapter runs with.
func (a *Adapter) Config() Config { return a.cfg }

// Unavailable says why the video model cannot be called now (domain.ExcludedNoBackend: it has no
// backend or no API key is stored for its vendor; domain.ExcludedCircuitOpen: its vendor's circuit
// breaker is open), or "" when it can. It does not consume the breaker's probe call.
func (a *Adapter) Unavailable(m domain.ModelName) string {
	backend := a.backends.Video[m]
	if backend == nil {
		return domain.ExcludedNoBackend
	}
	// Keys are added at run time, so the stored key is looked up on every call.
	if c, ok := backend.(interface{ Configured() bool }); ok && !c.Configured() {
		return domain.ExcludedNoBackend
	}
	if b := a.breakers[VendorOf(m)]; b == nil || b.IsOpen() {
		return domain.ExcludedCircuitOpen
	}
	return ""
}

// EstimateImage is the most one image of the purpose can cost: the dearer of the primary model and
// the alternative the call falls back to. It is what a budget check holds before the call.
func (a *Adapter) EstimateImage(purpose ImagePurpose) float64 {
	route := a.cfg.ImageRouteFor(purpose)
	return max(a.cfg.ImageCost(route.Primary.Model), a.cfg.ImageCost(route.Fallback.Model))
}

// GenerateImage makes an image with the primary model of the purpose, and with the alternative
// when the primary is unavailable (its breaker is open, or it failed after the retries).
func (a *Adapter) GenerateImage(ctx context.Context, purpose ImagePurpose, req ImageRequest) (ImageOutput, error) {
	route := a.cfg.ImageRouteFor(purpose)
	targets := []ImageTarget{route.Primary}
	if route.Fallback != route.Primary {
		targets = append(targets, route.Fallback)
	}
	if req.AspectRatio == "" {
		req.AspectRatio = domain.DefaultAspectRatio
	}
	var failures []error
	for i, t := range targets {
		backend := a.backends.Image[t.Model]
		if backend == nil {
			failures = append(failures, fmt.Errorf("%s: no backend", t.Model))
			continue
		}
		mc := a.cfg.Models[t.Model]
		apiModel := mc.Model
		if t.Fast {
			apiModel = mc.FastModel
		}
		treq := req
		if !AcceptsImageInputs(t.Model) && len(req.Inputs) > 0 {
			a.logger.Warn("image model cannot use reference images, generating from the prompt only", "model", t.Model)
			treq.Inputs = nil
		}
		res, err := call(ctx, a, VendorOf(t.Model), nil, func(ctx context.Context) (ImageResult, error) {
			return backend.GenerateImage(ctx, apiModel, treq)
		}, string(t.Model))
		if err == nil {
			return ImageOutput{ImageResult: res, Model: t.Model, FellBack: i > 0, EstimatedCost: a.cfg.ImageCost(t.Model)}, nil
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
	if a.timeLimit != nil {
		job.MaxTime = a.timeLimit(job.MaxTime)
	}
	targets := []VideoTarget{primary}
	if fallback != primary {
		targets = append(targets, fallback)
	}
	var failures []error
	clock := &limitClock{limit: job.MaxTime}
	for i, t := range targets {
		backend := a.backends.Video[t.Model]
		if backend == nil {
			failures = append(failures, fmt.Errorf("%s: no backend", t.Model))
			continue
		}
		req, err := Prepare(t, job)
		if err != nil {
			return VideoOutput{}, err
		}
		apiModel := a.cfg.Models[t.Model].Model
		res, err := call(ctx, a, VendorOf(t.Model), clock, func(ctx context.Context) (VideoResult, error) {
			return backend.GenerateVideo(ctx, apiModel, req)
		}, string(t.Model))
		if err == nil {
			return VideoOutput{
				VideoResult: res, Model: t.Model, Resolution: t.Resolution, DurationSeconds: req.DurationSeconds,
				Request: req, FellBack: i > 0, EstimatedCost: a.cfg.VideoCost(t, req.DurationSeconds),
			}, nil
		}
		if ctx.Err() != nil {
			return VideoOutput{}, ctx.Err()
		}
		if clock.expired() {
			// The alternative has no time left either: say that, not what the cut-off call returned.
			return VideoOutput{}, fmt.Errorf("%w (%s): %w", ErrTimeLimit, job.MaxTime, err)
		}
		failures = append(failures, fmt.Errorf("%s: %w", t.Model, err))
		a.logFailover(t.Model, i, len(targets), err)
	}
	return VideoOutput{}, fmt.Errorf("modeladapter: every video model failed: %w", routeFailure(failures))
}

// routeFailure joins what every model of a route failed with (see resilience.RouteFailure): only a
// route whose models were all skipped by their breakers is an ErrCircuitOpen.
func routeFailure(failures []error) error {
	return resilience.RouteFailure(failures, func(err error) bool { return errors.Is(err, ErrCircuitOpen) })
}

func (a *Adapter) logFailover(m domain.ModelName, i, n int, err error) {
	if i+1 < n {
		a.logger.Warn("model failed, routing to the alternative", "model", m, "err", err)
	}
}

// limitClock is the time limit of one job (VideoJob.MaxTime) over all its model calls. It starts at
// the first vendor concurrency slot the job gets, not when the job asks for it: the time spent
// waiting for other jobs to free a slot is not model time. A nil clock, or one without a limit,
// never expires.
type limitClock struct {
	limit    time.Duration
	deadline time.Time
	// ctx is the context the limit applied to last; it tells an expired limit from a cancelled call.
	ctx context.Context
}

// start binds ctx to the limit, starting the clock if it has not started yet. The returned cancel
// releases the bound context.
func (c *limitClock) start(ctx context.Context) (context.Context, context.CancelFunc) {
	if c == nil || c.limit <= 0 {
		return ctx, func() {}
	}
	if c.deadline.IsZero() {
		c.deadline = time.Now().Add(c.limit)
	}
	bound, cancel := context.WithDeadline(ctx, c.deadline)
	c.ctx = bound
	return bound, cancel
}

// expired reports whether the limit ran out (rather than the caller giving up).
func (c *limitClock) expired() bool {
	return c != nil && c.ctx != nil && errors.Is(c.ctx.Err(), context.DeadlineExceeded) && !c.deadline.IsZero()
}

// call runs fn against one vendor: it skips the vendor while its breaker is open, takes a
// concurrency slot, and retries unavailable answers with exponential backoff. A 429 also lowers
// the vendor's concurrency limit. The call is bounded by the vendor's timeout, and by clock, which
// starts once the slot is held.
func call[T any](ctx context.Context, a *Adapter, v Vendor, clock *limitClock, fn func(context.Context) (T, error), model string) (T, error) {
	var zero T
	b, lim := a.breakers[v], a.limiters[v]
	if b == nil || lim == nil {
		return zero, fmt.Errorf("modeladapter: vendor %q is not configured", v)
	}
	if b.IsOpen() {
		return zero, fmt.Errorf("%s: %w", model, ErrCircuitOpen)
	}
	release, err := lim.acquire(ctx)
	if err != nil {
		return zero, err
	}
	ok := false
	defer func() { release(ok) }()
	ctx, cancelLimit := clock.start(ctx)
	defer cancelLimit()

	policy := resilience.Policy{
		MaxAttempts: a.cfg.Retry.MaxAttempts,
		BaseDelay:   time.Duration(a.cfg.Retry.BaseDelayMillis) * time.Millisecond,
		MaxDelay:    time.Duration(a.cfg.Retry.MaxDelayMillis) * time.Millisecond,
	}
	hooks := resilience.Hooks{
		Unavailable: unavailable, Retryable: retryable, RetryAfter: retryAfter, Sleep: a.sleep,
		OnRetry: func(attempt int, wait time.Duration, err error) {
			if RateLimited(err) {
				lim.throttle()
			}
			a.logger.Warn("model call failed, retrying", "model", model, "attempt", attempt, "wait", wait, "err", err)
		},
	}
	timeout := a.cfg.timeout(v)
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
			lim.throttle()
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
	if l := a.limiters[v]; l != nil {
		return l.current()
	}
	return 0
}
