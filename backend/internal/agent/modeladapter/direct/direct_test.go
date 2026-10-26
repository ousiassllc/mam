package direct_test

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
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type fakeImage struct {
	mu       sync.Mutex
	err      error
	reqs     []modeladapter.ImageRequest
	models   []string
	block    chan struct{}
	inflight atomic.Int32
	peak     atomic.Int32
}

func (f *fakeImage) GenerateImage(ctx context.Context, model string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
	n := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for p := f.peak.Load(); n > p && !f.peak.CompareAndSwap(p, n); p = f.peak.Load() {
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return modeladapter.ImageResult{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	f.models = append(f.models, model)
	if f.err != nil {
		return modeladapter.ImageResult{}, f.err
	}
	return modeladapter.ImageResult{Data: []byte("png"), MimeType: "image/png"}, nil
}

func (f *fakeImage) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

type fakeVideo struct {
	mu   sync.Mutex
	reqs []modeladapter.VideoRequest
}

func (f *fakeVideo) GenerateVideo(_ context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("mp4")), MimeType: "video/mp4"}, nil
}

func perr(vendor modeladapter.Vendor, code int) error {
	return &modeladapter.ProviderError{Vendor: string(vendor), Status: code, Message: "boom"}
}

func noSleep(context.Context, time.Duration) error { return nil }

func adapter(cfg modeladapter.Config, b modeladapter.Backends) *modeladapter.Adapter {
	return modeladapter.New(cfg, b, modeladapter.WithSleep(noSleep))
}

func png() []modeladapter.Input {
	return []modeladapter.Input{{Data: []byte("x"), MimeType: "image/png"}}
}

func TestImageUsesTheNamedModelWithoutFallbackAndSendsThePromptAsItIs(t *testing.T) {
	flux := &fakeImage{err: perr(modeladapter.VendorFlux, http.StatusUnprocessableEntity)}
	other := &fakeImage{}
	a := adapter(modeladapter.DefaultConfig(), modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelFlux2: flux, domain.ModelNanoBananaLite: other, domain.ModelGPTImage2: other, domain.ModelNanoBanana: other,
	}})

	_, err := direct.Image(t.Context(), a, domain.ModelFlux2, modeladapter.ImageRequest{Prompt: "  a fox, raw  ", AspectRatio: "1:1"})
	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Equal(t, 1, flux.calls())
	assert.Zero(t, other.calls(), "no other model is tried: the preview route would fall back to Nano Banana Lite")
	assert.Equal(t, "  a fox, raw  ", flux.reqs[0].Prompt, "the prompt is not rewritten")
	assert.Equal(t, "flux-2-pro", flux.models[0])
}

func TestImageSeedIsSentOnlyWhereTheVendorTakesOne(t *testing.T) {
	flux, gpt := &fakeImage{}, &fakeImage{}
	a := adapter(modeladapter.DefaultConfig(), modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelFlux2: flux, domain.ModelGPTImage2: gpt,
	}})
	seed := int64(99)
	out, err := direct.Image(t.Context(), a, domain.ModelFlux2, modeladapter.ImageRequest{Prompt: "x", Seed: &seed})
	require.NoError(t, err)
	_, err = direct.Image(t.Context(), a, domain.ModelGPTImage2, modeladapter.ImageRequest{Prompt: "x", Seed: &seed, NegativePrompt: "no"})
	require.NoError(t, err)

	require.NotNil(t, flux.reqs[0].Seed)
	assert.Equal(t, int64(99), *flux.reqs[0].Seed)
	assert.Nil(t, gpt.reqs[0].Seed, "OpenAI has no seed field")
	assert.Empty(t, gpt.reqs[0].NegativePrompt, "no negative field: the caller folds it into the prompt")
	assert.Equal(t, domain.ModelFlux2, out.Model)
	assert.False(t, out.FellBack)
	assert.InDelta(t, 0.03, out.EstimatedCost, 1e-9)
	assert.Equal(t, modeladapter.DefaultConfig().Models[domain.ModelFlux2].CostPerImage, out.EstimatedCost)
}

func TestImageInputsForAModelThatTakesNoneAreRefusedNotDropped(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"oa": {Kind: localcfg.KindOpenAICompat, BaseURL: "http://localhost:1234"}}
	// accepts_image_inputs is false: this local model cannot use reference images.
	cfg.LocalModels = map[string]localcfg.Model{"oa/plain": {Media: localcfg.MediaImage, Model: "plain-model"}}
	const plainModel domain.ModelName = "openai-compat:oa/plain"
	plain := &fakeImage{}
	a := adapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{plainModel: plain}})
	_, err := direct.Image(t.Context(), a, plainModel, modeladapter.ImageRequest{Prompt: "x", Inputs: png()})
	assert.ErrorIs(t, err, direct.ErrInputsUnsupported)
	assert.Zero(t, plain.calls())

	_, err = direct.Image(t.Context(), a, plainModel, modeladapter.ImageRequest{Prompt: "x"})
	require.NoError(t, err, "the same model works from the prompt alone")
	assert.Equal(t, 1, plain.calls())

	_, err = direct.Image(t.Context(), a, domain.ModelFlux2, modeladapter.ImageRequest{Prompt: "x"})
	assert.ErrorIs(t, err, direct.ErrNoBackend)
}

func TestTheBreakerOfTheVendorStaysInForce(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.Breaker = modeladapter.BreakerConfig{FailureThreshold: 2, OpenSeconds: 3600}
	flux := &fakeImage{err: perr(modeladapter.VendorFlux, http.StatusServiceUnavailable)}
	a := adapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{domain.ModelFlux2: flux}})

	for range 2 {
		_, err := direct.Image(t.Context(), a, domain.ModelFlux2, modeladapter.ImageRequest{Prompt: "x"})
		require.Error(t, err)
	}
	assert.Equal(t, domain.ExcludedCircuitOpen, a.Unavailable(domain.ModelFlux2))
	_, err := direct.Image(t.Context(), a, domain.ModelFlux2, modeladapter.ImageRequest{Prompt: "x"})
	assert.ErrorIs(t, err, modeladapter.ErrCircuitOpen)
	assert.Equal(t, 2, flux.calls(), "an open breaker skips the vendor")
}

func TestTheConcurrencyLimitOfTheVendorStaysInForce(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	vc := cfg.Vendors[modeladapter.VendorFlux]
	vc.MaxConcurrency = 1
	cfg.Vendors[modeladapter.VendorFlux] = vc
	flux := &fakeImage{block: make(chan struct{})}
	a := adapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{domain.ModelFlux2: flux}})

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := direct.Image(t.Context(), a, domain.ModelFlux2, modeladapter.ImageRequest{Prompt: "x"})
			assert.NoError(t, err)
		}()
	}
	require.Eventually(t, func() bool { return flux.inflight.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	assert.Never(t, func() bool { return flux.inflight.Load() > 1 }, 100*time.Millisecond, 5*time.Millisecond, "a second call got through the limit")
	for range 3 {
		flux.block <- struct{}{}
	}
	wg.Wait()
	assert.Equal(t, int32(1), flux.peak.Load())
}

func TestVideoSnapsTheLengthChecksTheInputsAndKeepsTheSeedForRunwayOnly(t *testing.T) {
	runway, kling := &fakeVideo{}, &fakeVideo{}
	a := adapter(modeladapter.DefaultConfig(), modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{
		domain.ModelRunwayGen45: runway, domain.ModelKling3: kling,
	}})
	seed := int64(5)
	first := &modeladapter.Input{Data: []byte("f"), MimeType: "image/png"}

	out, err := direct.Video(t.Context(), a, modeladapter.VideoTarget{Model: domain.ModelRunwayGen45, Resolution: "720p"},
		modeladapter.VideoRequest{Prompt: "p", NegativePrompt: "n", DurationSeconds: 5.5, AspectRatio: "1:1", FirstFrame: first, Seed: &seed})
	require.NoError(t, err)
	out.File.Discard()
	assert.Equal(t, 6.0, out.DurationSeconds, "snapped up to a length the model makes")
	assert.InDelta(t, 0.12*6, out.EstimatedCost, 1e-9)
	require.NotNil(t, runway.reqs[0].Seed)
	assert.Empty(t, runway.reqs[0].NegativePrompt, "Runway has no negative field")
	assert.Equal(t, "p", runway.reqs[0].Prompt)

	out, err = direct.Video(t.Context(), a, modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"},
		modeladapter.VideoRequest{Prompt: "p", NegativePrompt: "n", DurationSeconds: 6, Seed: &seed})
	require.NoError(t, err)
	out.File.Discard()
	assert.Equal(t, 10.0, out.DurationSeconds)
	assert.Nil(t, kling.reqs[0].Seed)
	assert.Equal(t, "n", kling.reqs[0].NegativePrompt, "Kling has a negative field")

	for name, tc := range map[string]struct {
		target modeladapter.VideoTarget
		req    modeladapter.VideoRequest
		want   error
	}{
		"resolution":    {modeladapter.VideoTarget{Model: domain.ModelRunwayGen45, Resolution: "1080p"}, modeladapter.VideoRequest{DurationSeconds: 5}, direct.ErrResolution},
		"last frame":    {modeladapter.VideoTarget{Model: domain.ModelRunwayGen45, Resolution: "720p"}, modeladapter.VideoRequest{DurationSeconds: 5, FirstFrame: first, LastFrame: first}, direct.ErrInputsUnsupported},
		"free refs":     {modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, modeladapter.VideoRequest{DurationSeconds: 5, References: png()}, direct.ErrInputsUnsupported},
		"last alone":    {modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, modeladapter.VideoRequest{DurationSeconds: 5, LastFrame: first}, direct.ErrInputsConflict},
		"unknown model": {modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}, modeladapter.VideoRequest{DurationSeconds: 5}, direct.ErrNoBackend},
	} {
		_, err := direct.Video(t.Context(), a, tc.target, tc.req)
		assert.ErrorIs(t, err, tc.want, name)
	}
	assert.Len(t, runway.reqs, 1)
	assert.Len(t, kling.reqs, 1, "a refused request is never sent")
}

func TestFramesAndFreeReferencesCannotBeCombined(t *testing.T) {
	seedance := &fakeVideo{}
	a := adapter(modeladapter.DefaultConfig(), modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{domain.ModelSeedance2: seedance}})
	first := &modeladapter.Input{Data: []byte("f"), MimeType: "image/png"}
	_, err := direct.Video(t.Context(), a, modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"},
		modeladapter.VideoRequest{DurationSeconds: 5, FirstFrame: first, References: png()})
	assert.ErrorIs(t, err, direct.ErrInputsConflict)
	out, err := direct.Video(t.Context(), a, modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"},
		modeladapter.VideoRequest{DurationSeconds: 5, References: png()})
	require.NoError(t, err)
	out.File.Discard()
	assert.Len(t, seedance.reqs, 1)
}

func TestUnavailableLooksAtImageModelsToo(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	a := adapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{domain.ModelFlux2: &fakeImage{}}})
	assert.Empty(t, a.Unavailable(domain.ModelFlux2), "an image model with a backend is available")
	assert.Equal(t, domain.ExcludedNoBackend, a.Unavailable(domain.ModelNanoBanana), "an image model without a backend")
	assert.Equal(t, domain.ExcludedNoBackend, a.Unavailable(domain.ModelKling3))
}

func TestSeedAndNegativeFieldCapabilities(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	for m, want := range map[domain.ModelName]bool{
		domain.ModelFlux2: true, domain.ModelRunwayGen45: true, domain.ModelNanoBanana: false, domain.ModelGPTImage2: false,
		domain.ModelNanoBananaLite: false, domain.ModelKling3: false, domain.ModelSeedance2: false,
	} {
		assert.Equal(t, want, direct.SupportsSeed(cfg, m), m)
	}
	assert.True(t, direct.NegativeField(cfg, domain.ModelKling3))
	assert.False(t, direct.NegativeField(cfg, domain.ModelRunwayGen45))
	assert.False(t, direct.NegativeField(cfg, domain.ModelFlux2))
}

func TestFoldUsesTheAvoidSentence(t *testing.T) {
	assert.Equal(t, "a fox. Avoid: blurry, text.", direct.Fold("a fox.", " blurry, text. "))
	assert.Equal(t, "a fox.", direct.Fold("a fox.", "  "))
}
