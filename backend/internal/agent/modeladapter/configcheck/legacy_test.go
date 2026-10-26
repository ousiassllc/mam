package configcheck_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// loadJSON writes a models.json with the content and loads it with configcheck.Load.
func loadJSON(t *testing.T, content string) (modeladapter.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return configcheck.Load(path)
}

func TestLegacyImagen4PrimaryBecomesNanoBananaAndTheFallbackIsKept(t *testing.T) {
	cfg, err := loadJSON(t, `{"images":{"new_reference":{"primary":{"model":"imagen-4"},"fallback":{"model":"gpt-image-2"}}}}`)

	require.NoError(t, err)
	assert.Equal(t, modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: domain.ModelNanoBanana},
		Fallback: modeladapter.ImageTarget{Model: domain.ModelGPTImage2},
	}, cfg.Images[modeladapter.PurposeNewReference])
}

func TestLegacyImagen4FastBecomesNanoBananaLiteWithoutTheFastFlag(t *testing.T) {
	cfg, err := loadJSON(t, `{"images":{"preview":{"primary":{"model":"flux-2"},"fallback":{"model":"imagen-4","fast":true}}}}`)

	require.NoError(t, err)
	route := cfg.Images[modeladapter.PurposePreview]
	assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelFlux2}, route.Primary)
	assert.Equal(t, modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite, Fast: false}, route.Fallback)
}

func TestLegacyImagen4ModelsEntryIsDropped(t *testing.T) {
	cfg, err := loadJSON(t, `{"models":{"imagen-4":{"model":"imagen-4.0-generate-001","fast_model":"imagen-4.0-fast-generate-001","cost_per_image_usd":0.04}}}`)

	require.NoError(t, err)
	assert.NotContains(t, cfg.Models, domain.ModelName("imagen-4"))
	assert.Equal(t, modeladapter.DefaultConfig().Models, cfg.Models, "the built-in models are untouched")
}

func TestFileWithoutImagen4IsReadAsItIs(t *testing.T) {
	cfg, err := loadJSON(t, `{"images":{"preview":{"primary":{"model":"nano-banana-lite"},"fallback":{"model":"flux-2"}}}}`)

	require.NoError(t, err)
	defaults := modeladapter.DefaultConfig()
	assert.Equal(t, modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: domain.ModelNanoBananaLite},
		Fallback: modeladapter.ImageTarget{Model: domain.ModelFlux2},
	}, cfg.Images[modeladapter.PurposePreview], "the file's route is kept")
	for _, p := range []modeladapter.ImagePurpose{modeladapter.PurposeNewReference, modeladapter.PurposeEditReference, modeladapter.PurposeUnifiedOpenAI} {
		assert.Equal(t, defaults.Images[p], cfg.Images[p], "%s keeps the default", p)
	}
	assert.Equal(t, defaults.Models, cfg.Models)
}

func TestEmptyFileKeepsTheDefaultRoutes(t *testing.T) {
	cfg, err := loadJSON(t, `{}`)

	require.NoError(t, err)
	assert.Equal(t, modeladapter.DefaultConfig().Images, cfg.Images)
}

func TestLegacyRouteWithImagen4OnBothSidesLoads(t *testing.T) {
	cfg, err := loadJSON(t, `{"images":{"edit_reference":{"primary":{"model":"imagen-4"},"fallback":{"model":"imagen-4","fast":true}}}}`)

	require.NoError(t, err)
	route := cfg.Images[modeladapter.PurposeEditReference]
	assert.Equal(t, domain.ModelNanoBanana, route.Primary.Model)
	assert.Equal(t, domain.ModelNanoBananaLite, route.Fallback.Model)
	require.NoError(t, configcheck.Validate(cfg))

	cfg, err = loadJSON(t, `{"images":{"edit_reference":{"primary":{"model":"imagen-4"},"fallback":{"model":"imagen-4"}}}}`)
	require.NoError(t, err)
	route = cfg.Images[modeladapter.PurposeEditReference]
	assert.Equal(t, domain.ModelNanoBanana, route.Primary.Model)
	assert.Equal(t, domain.ModelNanoBanana, route.Fallback.Model, "both sides read as Nano Banana")
	require.NoError(t, configcheck.Validate(cfg))
}

func TestLegacyFileLoadsToAValidConfigAndDoesNotRewriteTheFile(t *testing.T) {
	content := `{
	  "models": {"imagen-4": {"model": "imagen-4.0-generate-001", "fast_model": "imagen-4.0-fast-generate-001", "cost_per_image_usd": 0.04}},
	  "images": {
	    "new_reference": {"primary": {"model": "imagen-4"}, "fallback": {"model": "gpt-image-2"}},
	    "preview": {"primary": {"model": "flux-2"}, "fallback": {"model": "imagen-4", "fast": true}}
	  }
	}`
	path := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := configcheck.Load(path)

	require.NoError(t, err)
	require.NoError(t, configcheck.Validate(cfg))
	for p, route := range cfg.Images {
		for _, tgt := range []modeladapter.ImageTarget{route.Primary, route.Fallback} {
			assert.NotEqual(t, domain.ModelName("imagen-4"), tgt.Model, "route %s", p)
			assert.True(t, cfg.AcceptsImages(tgt.Model), "route %s: %s is a built-in model", p, tgt.Model)
		}
	}
	onDisk, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(onDisk), "the file on disk is not rewritten")
	assert.NotContains(t, domain.ImageModels(), domain.ModelName("imagen-4"))
}
