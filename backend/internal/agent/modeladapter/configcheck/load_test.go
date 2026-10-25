package configcheck_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestDefaultConfigIsValid(t *testing.T) {
	require.NoError(t, configcheck.Validate(modeladapter.DefaultConfig()))
}

func TestConfigValidateRejectsUnsupportedRoutes(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Policies[domain.QualityFast] = modeladapter.PolicyRoute{
		Primary:  modeladapter.VideoTarget{Model: domain.ModelName("veo-3"), Resolution: "720p"}, // no adapter
		Fallback: modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "480p"},        // Kling has no 480p
		Takes:    0,
	}
	err := configcheck.Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "veo-3")
	assert.Contains(t, err.Error(), `"kling-3.0" does not support "480p"`)
	assert.Contains(t, err.Error(), "takes must be positive")
}

func TestConfigValidateRejectsBrokenRouting(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cfg.Routing.MinSamples = 0
	cfg.Routing.Weights[domain.QualityFast] = modeladapter.RoutingWeights{}
	cfg.Routing.Candidates[domain.QualityBalanced] = []modeladapter.VideoTarget{{Model: domain.ModelKling3, Resolution: "480p"}}

	err := configcheck.Validate(cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "routing weights fast")
	assert.Contains(t, err.Error(), "min_samples must be positive")
	assert.Contains(t, err.Error(), `"kling-3.0" does not support "480p"`)
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
		_, err := configcheck.Load(path)
		assert.Error(t, err, name)
	}
}

func TestConfigRejectsAQualityThatIsNotAProfile(t *testing.T) {
	for name, quality := range map[string]string{"auto": "auto", "unknown": "ultra"} {
		t.Run(name, func(t *testing.T) {
			cfg := modeladapter.DefaultConfig()
			mc := cfg.Models[domain.ModelGPTImage2]
			mc.Quality = quality
			cfg.Models[domain.ModelGPTImage2] = mc

			err := configcheck.Validate(cfg)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "quality")
		})
	}
	cfg := modeladapter.DefaultConfig()
	mc := cfg.Models[domain.ModelFlux2]
	mc.Quality = "high"
	cfg.Models[domain.ModelFlux2] = mc
	assert.ErrorContains(t, configcheck.Validate(cfg), "only gpt-image-2 has one")
}

func TestLoadWarnsWhenGPTImage2HasNoKnownPrice(t *testing.T) {
	for name, tc := range map[string]struct {
		models string
		warn   bool
	}{
		"the default":                   {`{}`, false},
		"a snapshot of the model":       {`{"models":{"gpt-image-2":{"model":"gpt-image-2-2026-04-21"}}}`, false},
		"another model without a price": {`{"models":{"gpt-image-2":{"model":"gpt-image-1.5"}}}`, true},
		"another model with a price":    {`{"models":{"gpt-image-2":{"model":"gpt-image-1.5","cost_per_image_usd":0.04}}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			path := filepath.Join(t.TempDir(), "models.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.models), 0o600))

			_, err := configcheck.Load(path)

			require.NoError(t, err, "a warning, not a refusal")
			if tc.warn {
				assert.Contains(t, logs.String(), "level=WARN")
				assert.Contains(t, logs.String(), "cost_per_image_usd")
				return
			}
			assert.Empty(t, logs.String())
		})
	}
}
