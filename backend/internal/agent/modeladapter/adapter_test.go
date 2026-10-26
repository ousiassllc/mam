package modeladapter_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// fakeVideo answers from a script: each call takes the next result (the last one repeats).
type fakeVideo struct {
	mu       sync.Mutex
	results  []error // nil = success
	calls    int
	requests []modeladapter.VideoRequest
	models   []string
	block    chan struct{} // when set, calls wait for it
	inflight atomic.Int32
	peak     atomic.Int32
}

func (f *fakeVideo) GenerateVideo(ctx context.Context, model string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	n := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return modeladapter.VideoResult{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.models = append(f.models, model)
	f.requests = append(f.requests, req)
	i := min(f.calls, len(f.results)-1)
	f.calls++
	if err := f.results[i]; err != nil {
		return modeladapter.VideoResult{}, err
	}
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("mp4 " + model)), MimeType: "video/mp4"}, nil
}

func (f *fakeVideo) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeImage struct {
	mu     sync.Mutex
	err    error
	calls  int
	inputs []int
	models []string
}

func (f *fakeImage) GenerateImage(_ context.Context, model string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.models = append(f.models, model)
	f.inputs = append(f.inputs, len(req.Inputs))
	if f.err != nil {
		return modeladapter.ImageResult{}, f.err
	}
	return modeladapter.ImageResult{Data: []byte("png " + model), MimeType: "image/png"}, nil
}

func perr(vendor modeladapter.Vendor, code int) error {
	return &modeladapter.ProviderError{Vendor: string(vendor), Status: code, Message: "boom"}
}

func noSleep(context.Context, time.Duration) error { return nil }

func baseJob() modeladapter.VideoJob {
	return modeladapter.VideoJob{
		Spec: domain.ShotSpec{
			ShotObjective: "A woman notices something behind her and starts running",
			Camera:        domain.SpecCamera{Framing: "medium"},
		},
		DurationSeconds: 4,
	}
}

var (
	klingTarget    = modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}
	seedanceTarget = modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}
	runwayTarget   = modeladapter.VideoTarget{Model: domain.ModelRunwayGen45, Resolution: "720p"}
)

func newAdapter(cfg modeladapter.Config, b modeladapter.Backends, opts ...modeladapter.Option) *modeladapter.Adapter {
	return modeladapter.New(cfg, b, append([]modeladapter.Option{modeladapter.WithSleep(noSleep)}, opts...)...)
}

func TestGenerateVideoFallsBackToTheAlternativeModelWithItsOwnPrompt(t *testing.T) {
	primary := &fakeVideo{results: []error{perr(modeladapter.VendorSeedance, http.StatusInternalServerError)}}
	fallback := &fakeVideo{results: []error{nil}}
	b := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: primary, domain.ModelKling3: fallback,
	}}
	a := newAdapter(modeladapter.DefaultConfig(), b)

	out, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())

	require.NoError(t, err)
	assert.Equal(t, domain.ModelKling3, out.Model)
	assert.True(t, out.FellBack)
	assert.Equal(t, 3, primary.callCount(), "the primary is retried with backoff before the alternative is used")
	assert.Equal(t, 5.0, out.DurationSeconds, "Kling produces 5 or 10 seconds: the 4 second Shot is generated as 5")
	assert.InDelta(t, 5*0.07, out.EstimatedCost, 1e-9)
	assert.NotEmpty(t, fallback.requests[0].NegativePrompt, "Kling takes a negative prompt, rendered for it")
	assert.Equal(t, "kling-v3", fallback.models[0])
}

func TestGenerateVideoDoesNotRetryATimeoutButUsesTheAlternative(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	primary := &fakeVideo{results: []error{modeladapter.ErrTimeout}}
	fallback := &fakeVideo{results: []error{nil}}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: primary, domain.ModelKling3: fallback,
	}})

	out, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())

	require.NoError(t, err)
	assert.True(t, out.FellBack)
	assert.Equal(t, 1, primary.callCount(), "a timed-out job may still be billed at the vendor: it is not submitted twice")
}

func TestGenerateVideoDoesNotRetryARejectedRequest(t *testing.T) {
	primary := &fakeVideo{results: []error{perr(modeladapter.VendorSeedance, http.StatusUnprocessableEntity)}}
	fallback := &fakeVideo{results: []error{perr(modeladapter.VendorKling, http.StatusUnprocessableEntity)}}
	a := newAdapter(modeladapter.DefaultConfig(), modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: primary, domain.ModelKling3: fallback,
	}})

	_, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())

	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Equal(t, 1, primary.callCount())
	assert.Equal(t, 1, fallback.callCount())
}

func TestCircuitBreakerSkipsAnUnavailableVendor(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 2
	primary := &fakeVideo{results: []error{perr(modeladapter.VendorSeedance, http.StatusBadGateway)}}
	fallback := &fakeVideo{results: []error{nil}}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: primary, domain.ModelKling3: fallback,
	}}, modeladapter.WithClock(clock))

	for range 3 {
		_, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())
		require.NoError(t, err)
	}
	assert.Equal(t, 2, primary.callCount(), "after 2 failures the breaker is open: the third call goes straight to the alternative")

	mu.Lock()
	now = now.Add(time.Duration(cfg.Breaker.OpenSeconds+1) * time.Second)
	mu.Unlock()
	primary.mu.Lock()
	primary.results = []error{nil}
	primary.mu.Unlock()
	out, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())
	require.NoError(t, err)
	assert.Equal(t, domain.ModelSeedance2, out.Model, "a probe after the open period closes the breaker again")
}

func TestGenerateVideoFailsWhenEveryModelFails(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	f := &fakeVideo{results: []error{perr(modeladapter.VendorKling, http.StatusTooManyRequests)}}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: f, domain.ModelKling3: f,
	}})

	_, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())

	require.Error(t, err)
	assert.True(t, modeladapter.RateLimited(err), "429 is reported so the caller can tell the user to wait")
}

func TestConcurrencyIsLimitedPerVendorAndLoweredOn429(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	vc := cfg.Vendors[modeladapter.VendorSeedance]
	vc.MaxConcurrency = 2
	cfg.Vendors[modeladapter.VendorSeedance] = vc
	f := &fakeVideo{results: []error{nil}, block: make(chan struct{})}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelSeedance2: f}})

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_, err := a.GenerateVideo(context.Background(), seedanceTarget, seedanceTarget, baseJob())
			assert.NoError(t, err)
		})
	}
	require.Eventually(t, func() bool { return f.inflight.Load() == 2 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.EqualValues(t, 2, f.inflight.Load(), "no more than max_concurrency calls run at once")
	close(f.block)
	wg.Wait()
	assert.EqualValues(t, 2, f.peak.Load())
}

func TestRateLimitHalvesTheConcurrencyLimit(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	vc := cfg.Vendors[modeladapter.VendorSeedance]
	vc.MaxConcurrency = 4
	cfg.Vendors[modeladapter.VendorSeedance] = vc
	f := &fakeVideo{results: []error{perr(modeladapter.VendorSeedance, http.StatusTooManyRequests), nil}, block: make(chan struct{})}
	close(f.block)
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelSeedance2: f}})

	_, err := a.GenerateVideo(context.Background(), seedanceTarget, seedanceTarget, baseJob())
	require.NoError(t, err)
	assert.Equal(t, 2, a.ConcurrencyLimit(modeladapter.VendorSeedance), "one 429 halves the limit of 4")
}

func TestGenerateImageRoutesByPurposeAndFallsBack(t *testing.T) {
	gpt := &fakeImage{err: perr(modeladapter.VendorOpenAI, http.StatusServiceUnavailable)}
	nano := &fakeImage{}
	flux := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelGPTImage2: gpt, domain.ModelNanoBanana: nano, domain.ModelFlux2: flux,
	}})
	ctx := context.Background()

	out, err := a.GenerateImage(ctx, modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "a product on white"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelNanoBanana, out.Model, "GPT Image 2 is unavailable: Nano Banana is the alternative for new references")
	assert.True(t, out.FellBack)
	assert.Equal(t, []string{"gemini-nano-banana-2.1"}, nano.models)
	assert.InDelta(t, 0.0336, out.EstimatedCost, 1e-9)

	out, err = a.GenerateImage(ctx, modeladapter.PurposeEditReference, modeladapter.ImageRequest{Prompt: "style", Inputs: []modeladapter.Input{{Data: []byte("x"), MimeType: "image/png"}}})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelNanoBanana, out.Model)
	assert.False(t, out.FellBack)
	assert.Equal(t, []int{0, 1}, nano.inputs, "the edit keeps its reference image")

	out, err = a.GenerateImage(ctx, modeladapter.PurposePreview, modeladapter.ImageRequest{Prompt: "quick"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelFlux2, out.Model)
	assert.InDelta(t, 0.03, out.EstimatedCost, 1e-9)
}
