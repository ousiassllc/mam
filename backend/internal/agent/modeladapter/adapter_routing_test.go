package modeladapter_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestPreferOpenAIRoutesNewReferencesThroughGPTImage(t *testing.T) {
	gpt := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.PreferOpenAI = true
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelGPTImage2: gpt, domain.ModelNanoBanana: &fakeImage{},
	}})
	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelGPTImage2, out.Model)
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
