package imagespec_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestDefaultConfigNamesTheCurrentModelsAndNoRetiredOnes(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Equal(t, "gpt-image-2", cfg.Models[domain.ModelGPTImage2].Model)
	assert.Equal(t, "gemini-nano-banana-2.1", cfg.Models[domain.ModelNanoBanana].Model)
	assert.Equal(t, "gemini-3.1-flash-lite-image", cfg.Models[domain.ModelNanoBananaLite].Model)

	retired := []string{"imagen", "gpt-image-1", "gemini-2.5-flash-image"}
	hasRetired := func(s string) bool {
		for _, r := range retired {
			if strings.Contains(strings.ToLower(s), r) {
				return true
			}
		}
		return false
	}
	for name, mc := range cfg.Models {
		assert.False(t, hasRetired(string(name)), "model %s", name)
		assert.False(t, hasRetired(mc.Model), "api id of %s: %s", name, mc.Model)
		assert.False(t, hasRetired(mc.FastModel), "fast api id of %s: %s", name, mc.FastModel)
		assert.Empty(t, mc.FastModel, "no built-in model has a fast variant: %s", name)
	}
	for purpose, route := range cfg.Images {
		for _, tgt := range []modeladapter.ImageTarget{route.Primary, route.Fallback} {
			assert.False(t, hasRetired(string(tgt.Model)), "route %s: %s", purpose, tgt.Model)
			assert.False(t, tgt.Fast, "route %s: no fast target", purpose)
		}
	}
	assert.NotContains(t, domain.ImageModels(), domain.ModelName("imagen-4"))
}

// The assignment of spec/architecture/model-adapters.md §3 (decision of Issue #837).
func TestDefaultImageRoutes(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	for _, tt := range []struct {
		purpose         modeladapter.ImagePurpose
		primary, backup domain.ModelName
	}{
		{modeladapter.PurposeNewReference, domain.ModelGPTImage2, domain.ModelNanoBanana},
		{modeladapter.PurposeEditReference, domain.ModelNanoBanana, domain.ModelGPTImage2},
		{modeladapter.PurposePreview, domain.ModelFlux2, domain.ModelNanoBananaLite},
		{modeladapter.PurposeUnifiedOpenAI, domain.ModelGPTImage2, domain.ModelNanoBanana},
	} {
		t.Run(string(tt.purpose), func(t *testing.T) {
			assert.Equal(t, modeladapter.ImageRoute{
				Primary: modeladapter.ImageTarget{Model: tt.primary}, Fallback: modeladapter.ImageTarget{Model: tt.backup},
			}, cfg.ImageRouteFor(tt.purpose))
		})
	}
	assert.Len(t, cfg.Images, len(modeladapter.ImagePurposes()), "no purpose beyond the four")
}

type fakeImage struct {
	err    error
	inputs []int
	models []string
}

func (f *fakeImage) GenerateImage(_ context.Context, model string, req modeladapter.ImageRequest) (modeladapter.ImageResult, error) {
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

func newAdapter(cfg modeladapter.Config, b modeladapter.Backends) *modeladapter.Adapter {
	return modeladapter.New(cfg, b, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
}

// withNoReferenceModel adds "openai-compat:oa/plain", an image model that takes no reference images
// (accepts_image_inputs is false), and returns its identifier.
func withNoReferenceModel(cfg *modeladapter.Config) domain.ModelName {
	cfg.Endpoints = map[string]localcfg.Endpoint{"oa": {Kind: localcfg.KindOpenAICompat, BaseURL: "http://localhost:1234"}}
	cfg.LocalModels = map[string]localcfg.Model{"oa/plain": {Media: localcfg.MediaImage, Model: "plain-model"}}
	return "openai-compat:oa/plain"
}

func TestImageFallbackWithoutReferenceInputsStillGenerates(t *testing.T) {
	nano := &fakeImage{err: perr(modeladapter.VendorGoogle, http.StatusBadGateway)}
	plain := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	local := withNoReferenceModel(&cfg)
	cfg.Images[modeladapter.PurposeEditReference] = modeladapter.ImageRoute{
		Primary: modeladapter.ImageTarget{Model: domain.ModelNanoBanana}, Fallback: modeladapter.ImageTarget{Model: local},
	}
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelNanoBanana: nano, local: plain,
	}})

	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeEditReference,
		modeladapter.ImageRequest{Prompt: "lighting", Inputs: []modeladapter.Input{{Data: []byte("x"), MimeType: "image/png"}}})

	require.NoError(t, err)
	assert.Equal(t, local, out.Model)
	assert.Equal(t, []int{1}, nano.inputs, "the primary takes the reference image")
	assert.Equal(t, []int{0}, plain.inputs, "a model without reference input gets the prompt only")
}

func TestFastPreviewFallbackUsesTheConfiguredFastModel(t *testing.T) {
	flux := &fakeImage{err: perr(modeladapter.VendorFlux, http.StatusBadGateway)}
	lite := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	// No built-in model has a fast variant: a user sets one with fast_model in models.json.
	mc := cfg.Models[domain.ModelNanoBananaLite]
	mc.FastModel = "gemini-fast-lite"
	cfg.Models[domain.ModelNanoBananaLite] = mc
	cfg.Images[modeladapter.PurposePreview] = modeladapter.ImageRoute{
		Primary: modeladapter.ImageTarget{Model: domain.ModelFlux2}, Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite, Fast: true},
	}
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelFlux2: flux, domain.ModelNanoBananaLite: lite,
	}})
	_, err := a.GenerateImage(context.Background(), modeladapter.PurposePreview, modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, []string{"gemini-fast-lite"}, lite.models)
}

func TestPreviewFallbackUsesTheStandardModelOfNanoBananaLite(t *testing.T) {
	flux := &fakeImage{err: perr(modeladapter.VendorFlux, http.StatusBadGateway)}
	lite := &fakeImage{}
	cfg := modeladapter.DefaultConfig()
	cfg.Retry.MaxAttempts = 1
	a := newAdapter(cfg, modeladapter.Backends{Image: map[domain.ModelName]modeladapter.ImageBackend{
		domain.ModelFlux2: flux, domain.ModelNanoBananaLite: lite,
	}})
	out, err := a.GenerateImage(context.Background(), modeladapter.PurposePreview, modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelNanoBananaLite, out.Model)
	assert.Equal(t, []string{"gemini-3.1-flash-lite-image"}, lite.models)
}
