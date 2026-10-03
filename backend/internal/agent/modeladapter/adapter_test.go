package modeladapter_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
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

func TestDefaultConfigIsValid(t *testing.T) {
	require.NoError(t, modeladapter.DefaultConfig().Validate())
}

func TestConfigValidateRejectsUnsupportedRoutes(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: domain.ModelName("veo-3"), Resolution: "720p"}, // no adapter
		Fallback: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "480p"},        // Kling has no 480p
		Takes:    0,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "veo-3")
	assert.Contains(t, err.Error(), `"kling-3.0" does not support "480p"`)
	assert.Contains(t, err.Error(), "takes must be positive")
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
	imagen := &fakeImage{err: perr(modeladapter.VendorGoogle, http.StatusServiceUnavailable)}
	gpt := &fakeImage{}
	nano := &fakeImage{}
	flux := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelImagen4: imagen, domain.ModelGPTImage2: gpt, domain.ModelNanoBanana: nano, domain.ModelFlux2: flux,
	}})
	ctx := context.Background()

	out, err := a.GenerateImage(ctx, modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "a product on white"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelGPTImage2, out.Model, "Imagen 4 is unavailable: GPT Image 2 is the alternative for new references")
	assert.True(t, out.FellBack)

	out, err = a.GenerateImage(ctx, modeladapter.PurposeEditReference, modeladapter.ImageRequest{Prompt: "style", Inputs: []modeladapter.Input{{Data: []byte("x"), MimeType: "image/png"}}})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelNanoBanana, out.Model)
	assert.Equal(t, []int{1}, nano.inputs)

	out, err = a.GenerateImage(ctx, modeladapter.PurposePreview, modeladapter.ImageRequest{Prompt: "quick"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelFlux2, out.Model)
	assert.InDelta(t, 0.03, out.EstimatedCost, 1e-9)
}

func TestImageFallbackWithoutReferenceInputsStillGenerates(t *testing.T) {
	nano := &fakeImage{err: perr(modeladapter.VendorGoogle, http.StatusBadGateway)}
	imagen := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelNanoBanana: nano, domain.ModelImagen4: imagen,
	}})

	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeEditReference,
		modeladapter.ImageRequest{Prompt: "lighting", Inputs: []modeladapter.Input{{Data: []byte("x"), MimeType: "image/png"}}})

	require.NoError(t, err)
	assert.Equal(t, domain.ModelImagen4, out.Model)
	assert.Equal(t, []int{0}, imagen.inputs, "Imagen 4 takes no reference images")
}

func TestPreferOpenAIRoutesNewReferencesThroughGPTImage(t *testing.T) {
	gpt := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.PreferOpenAI = true
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelGPTImage2: gpt, domain.ModelImagen4: &fakeImage{},
	}})
	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelGPTImage2, out.Model)
}

func TestFastPreviewFallbackUsesImagenFastModel(t *testing.T) {
	flux := &fakeImage{err: perr(modeladapter.VendorFlux, http.StatusBadGateway)}
	imagen := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelFlux2: flux, domain.ModelImagen4: imagen,
	}})
	_, err := a.GenerateImage(context.Background(), modeladapter.PurposePreview, modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, []string{"imagen-4.0-fast-generate-001"}, imagen.models)
}

func TestMaxQualityUsesRunwayAndDifferentModelSwitchesToKling(t *testing.T) {
	route := modeladapter.DefaultConfig().Policies[domain.QualityMaxQuality]
	assert.Equal(t, domain.ModelRunwayGen45, route.Primary.Model)
	assert.Equal(t, domain.ModelKling3, route.Fallback.Model)

	runway := &fakeVideo{results: []error{perr(modeladapter.VendorRunway, http.StatusServiceUnavailable)}}
	kling := &fakeVideo{results: []error{nil}}
	b := modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelRunwayGen45: runway, domain.ModelKling3: kling}}
	a := newAdapter(modeladapter.DefaultConfig(), b)

	out, err := a.GenerateVideo(context.Background(), route.Primary, route.Fallback, baseJob())

	require.NoError(t, err)
	assert.Equal(t, domain.ModelKling3, out.Model)
	assert.True(t, out.FellBack)
	assert.Equal(t, []string{"gen4.5"}, runway.models[:1])
}

func TestRunwayCallsAreLimitedByTheVendorConcurrency(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	vc := cfg.Vendors[modeladapter.VendorRunway]
	vc.MaxConcurrency = 2
	cfg.Vendors[modeladapter.VendorRunway] = vc
	runway := &fakeVideo{results: []error{nil}, block: make(chan struct{})}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelRunwayGen45: runway}})

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { _, _ = a.GenerateVideo(context.Background(), runwayTarget, runwayTarget, baseJob()) })
	}
	require.Eventually(t, func() bool { return runway.peak.Load() == 2 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	assert.EqualValues(t, 2, runway.peak.Load())
	close(runway.block)
	wg.Wait()
	assert.Equal(t, 5, runway.callCount())
}

func TestUnavailableNamesTheReasonAModelCannotBeCalled(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 1
	seedance := &fakeVideo{results: []error{perr(modeladapter.VendorSeedance, http.StatusBadGateway)}}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: seedance, domain.ModelKling3: &fakeVideo{results: []error{nil}},
	}})
	assert.Empty(t, a.Unavailable(domain.ModelSeedance2))
	assert.Equal(t, domain.ExcludedNoBackend, a.Unavailable(domain.ModelRunwayGen45), "no API key, no backend")

	_, err := a.GenerateVideo(context.Background(), seedanceTarget, seedanceTarget, baseJob())
	require.Error(t, err)

	assert.Equal(t, domain.ExcludedCircuitOpen, a.Unavailable(domain.ModelSeedance2), "the vendor's breaker opened")
	assert.Empty(t, a.Unavailable(domain.ModelKling3), "another vendor is not affected")
}

func TestConfigValidateRejectsBrokenRouting(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Routing.MinSamples = 0
	cfg.Routing.Weights[domain.QualityFast] = modeladapter.RoutingWeights{}
	cfg.Routing.Candidates[domain.QualityBalanced] = []modeladapter.VideoTarget{{Model: domain.ModelKling3, Resolution: "480p"}}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "routing weights fast")
	assert.Contains(t, err.Error(), "min_samples must be positive")
	assert.Contains(t, err.Error(), `"kling-3.0" does not support "480p"`)
}

func TestLoadConfigOverridesRoutingAndKeepsTheRestOfTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"routing":{"enabled":false,"min_samples":9,"weights":{"fast":{"quality":1,"cost":0,"fit":0}}}}`), 0o600))

	cfg, err := modeladapter.LoadConfig(path)

	require.NoError(t, err)
	assert.False(t, cfg.Routing.Enabled)
	assert.Equal(t, 9, cfg.Routing.MinSamples)
	assert.Equal(t, modeladapter.RoutingWeights{Quality: 1}, cfg.Routing.Weights[domain.QualityFast])
	assert.Equal(t, 0.6, cfg.Routing.Weights[domain.QualityBalanced].Quality, "the other policies keep their default weights")
	assert.Len(t, cfg.Routing.Candidates[domain.QualityMaxQuality], 3)

	require.NoError(t, os.WriteFile(path, []byte(`{"routing":{"enable":true}}`), 0o600))
	_, err = modeladapter.LoadConfig(path)
	assert.ErrorContains(t, err, "unknown field")
}

func TestLoadConfigMergesAnEntryThatNamesOnlySomeOfItsFields(t *testing.T) {
	def := modeladapter.DefaultConfig()
	load := func(t *testing.T, body string) modeladapter.Config {
		t.Helper()
		path := filepath.Join(t.TempDir(), "models.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		cfg, err := modeladapter.LoadConfig(path)
		require.NoError(t, err)
		return cfg
	}

	t.Run("a model keeps its price", func(t *testing.T) {
		cfg := load(t, `{"models":{"gpt-image-2":{"model":"gpt-image-2"}}}`)
		assert.Equal(t, "gpt-image-2", cfg.Models[domain.ModelGPTImage2].Model)
		assert.Equal(t, def.Models[domain.ModelGPTImage2].CostPerImage, cfg.Models[domain.ModelGPTImage2].CostPerImage)
		assert.Equal(t, def.ImageCost(domain.ModelGPTImage2), cfg.ImageCost(domain.ModelGPTImage2), "the estimate still counts in the ledger")
	})
	t.Run("a model's video prices are merged by resolution", func(t *testing.T) {
		cfg := load(t, `{"models":{"kling-3.0":{"cost_per_second_usd":{"720p":0.5}}}}`)
		assert.Equal(t, map[string]float64{"720p": 0.5, "1080p": 0.14}, cfg.Models[domain.ModelKling3].CostPerSecond)
		assert.Equal(t, "kling-v3", cfg.Models[domain.ModelKling3].Model)
	})
	t.Run("a policy keeps its models", func(t *testing.T) {
		cfg := load(t, `{"policies":{"fast":{"takes":3}}}`)
		want := def.Policies[domain.QualityFast]
		want.Takes = 3
		assert.Equal(t, want, cfg.Policies[domain.QualityFast])
	})
	t.Run("a policy's route keeps the fields it does not name", func(t *testing.T) {
		cfg := load(t, `{"policies":{"fast":{"primary":{"resolution":"1080p"}}}}`)
		assert.Equal(t, modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"}, cfg.Policies[domain.QualityFast].Primary)
		assert.Equal(t, def.Policies[domain.QualityFast].Fallback, cfg.Policies[domain.QualityFast].Fallback)
	})
	t.Run("a vendor keeps its URL and concurrency", func(t *testing.T) {
		cfg := load(t, `{"vendors":{"kling":{"timeout_seconds":300}}}`)
		want := def.Vendors[modeladapter.VendorKling]
		want.TimeoutSeconds = 300
		assert.Equal(t, want, cfg.Vendors[modeladapter.VendorKling])
	})
	t.Run("an image route keeps its fallback", func(t *testing.T) {
		cfg := load(t, `{"images":{"preview":{"primary":{"model":"nano-banana"}}}}`)
		assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelNanoBanana}, cfg.Images[modeladapter.PurposePreview].Primary)
		assert.Equal(t, def.Images[modeladapter.PurposePreview].Fallback, cfg.Images[modeladapter.PurposePreview].Fallback)
	})
	t.Run("a policy's routing weights keep the fields they do not name", func(t *testing.T) {
		cfg := load(t, `{"routing":{"weights":{"fast":{"quality":0.9}}}}`)
		assert.Equal(t, modeladapter.RoutingWeights{Quality: 0.9, Cost: 0.5, Fit: 0.1}, cfg.Routing.Weights[domain.QualityFast])
		assert.Equal(t, def.Routing.Weights[domain.QualityBalanced], cfg.Routing.Weights[domain.QualityBalanced])
		assert.Equal(t, def.Routing.MinSamples, cfg.Routing.MinSamples)
		assert.Equal(t, def.Routing.Candidates, cfg.Routing.Candidates)
	})
	t.Run("an unknown field of an entry is still rejected", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "models.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"vendors":{"kling":{"bogus":1}}}`), 0o600))
		_, err := modeladapter.LoadConfig(path)
		assert.ErrorContains(t, err, "unknown field")
	})
}

func TestLoadConfigRejectsAnUnknownMapKey(t *testing.T) {
	for name, content := range map[string]string{
		"policy":            `{"policies":{"balance":{"takes":1}}}`,
		"image purpose":     `{"images":{"previw":{"primary":{"model":"flux-2"}}}}`,
		"model":             `{"models":{"kling-3":{"model":"x"}}}`,
		"cost resolution":   `{"models":{"kling-3.0":{"cost_per_second_usd":{"1080P":0.2}}}}`,
		"routing weights":   `{"routing":{"weights":{"max-quality":{"quality":1}}}}`,
		"routing candidate": `{"routing":{"candidates":{"fastt":[]}}}`,
		"vendor":            `{"vendors":{"klingg":{"base_url":"http://x","max_concurrency":1,"timeout_seconds":1}}}`,
	} {
		path := filepath.Join(t.TempDir(), "models.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, err := modeladapter.LoadConfig(path)
		assert.Error(t, err, name)
	}
}

func TestGenerateVideoFailOverModelGetsItsOwnInputWhenReferencesArePreferred(t *testing.T) {
	first := modeladapter.Input{Data: []byte("key"), MimeType: "image/png"}
	job := baseJob()
	job.FirstFrame = &first
	job.References = []modeladapter.Input{{Data: []byte("ref"), MimeType: "image/png"}}
	job.PreferReferences = true
	primary := &fakeVideo{results: []error{modeladapter.ErrTimeout}}
	fallback := &fakeVideo{results: []error{nil}}
	a := newAdapter(modeladapter.DefaultConfig(), modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: primary, domain.ModelKling3: fallback,
	}})

	out, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, job)

	require.NoError(t, err)
	assert.True(t, out.FellBack)
	assert.Nil(t, primary.requests[0].FirstFrame)
	assert.NotEmpty(t, primary.requests[0].References)
	assert.NotNil(t, fallback.requests[0].FirstFrame, "Kling has no free references: the Keyframe is its input")
}

// A vendor skipped for its open breaker makes the whole route ErrCircuitOpen ("wait and retry") only when
// every model was skipped; when the alternative was tried and failed for its own reason, that reason is
// what the caller sees (#439).
func TestGenerateVideoNamesTheAlternativesRealFailureWhenThePrimaryIsSkipped(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 1
	primary := &fakeVideo{results: []error{perr(modeladapter.VendorSeedance, http.StatusBadGateway)}}
	fallback := &fakeVideo{results: []error{perr(modeladapter.VendorKling, http.StatusUnprocessableEntity)}}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: primary, domain.ModelKling3: fallback,
	}})
	_, err := a.GenerateVideo(context.Background(), seedanceTarget, seedanceTarget, baseJob()) // opens the Seedance breaker
	require.Error(t, err)

	_, err = a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())

	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.NotErrorIs(t, err, modeladapter.ErrCircuitOpen)
	assert.Contains(t, err.Error(), "circuit breaker is open", "the skip stays in the message for the log")
	assert.Equal(t, 1, primary.callCount(), "the open breaker kept the primary from being called")
}

func TestGenerateVideoIsCircuitOpenWhenEveryModelOfTheRouteIsSkipped(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker.FailureThreshold = 1
	bad := func(v modeladapter.Vendor) *fakeVideo {
		return &fakeVideo{results: []error{perr(v, http.StatusBadGateway)}}
	}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: bad(modeladapter.VendorSeedance), domain.ModelKling3: bad(modeladapter.VendorKling),
	}})
	_, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob()) // opens both breakers
	require.Error(t, err)

	_, err = a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, baseJob())

	assert.ErrorIs(t, err, modeladapter.ErrCircuitOpen)
}

func TestGenerateVideoTimeLimitStartsOnceTheVendorSlotIsHeld(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	vc := cfg.Vendors[modeladapter.VendorSeedance]
	vc.MaxConcurrency = 1
	cfg.Vendors[modeladapter.VendorSeedance] = vc
	f := &fakeVideo{results: []error{nil}, block: make(chan struct{})}
	a := newAdapter(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelSeedance2: f}})

	var wg sync.WaitGroup
	wg.Go(func() { // takes the only slot
		_, err := a.GenerateVideo(context.Background(), seedanceTarget, seedanceTarget, baseJob())
		assert.NoError(t, err)
	})
	require.Eventually(t, func() bool { return f.inflight.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	job := baseJob()
	job.MaxTime = 100 * time.Millisecond
	errc := make(chan error, 1)
	go func() {
		_, err := a.GenerateVideo(context.Background(), seedanceTarget, seedanceTarget, job)
		errc <- err
	}()
	time.Sleep(300 * time.Millisecond) // three times its limit, all of it spent waiting for the slot
	close(f.block)

	assert.NoError(t, <-errc, "the wait for a slot is not model time")
	wg.Wait()
}

func TestGenerateVideoFailsWithTheTimeLimitWhenTheModelIsTooSlowAndSkipsTheAlternative(t *testing.T) {
	f := &fakeVideo{results: []error{nil}, block: make(chan struct{})}
	a := newAdapter(modeladapter.DefaultConfig(), modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelSeedance2: f, domain.ModelKling3: f,
	}})
	job := baseJob()
	job.MaxTime = 50 * time.Millisecond

	_, err := a.GenerateVideo(context.Background(), seedanceTarget, klingTarget, job)

	require.ErrorIs(t, err, modeladapter.ErrTimeLimit)
	assert.NotContains(t, err.Error(), string(domain.ModelKling3), "the alternative is not tried with no time left")
}
