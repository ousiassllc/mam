package modeladapter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// brokenConfig has several mistakes in every map Validate ranges over.
func brokenConfig() Config {
	cfg := DefaultConfig()
	cfg.Vendors[Vendor("zeta")] = VendorConfig{}
	cfg.Vendors[Vendor("alpha")] = VendorConfig{}
	cfg.Vendors[Vendor("mid")] = VendorConfig{}
	cfg.Models[domain.ModelName("typo-b")] = ModelConfig{}
	cfg.Models[domain.ModelName("typo-a")] = ModelConfig{}
	cfg.Models[domain.ModelKling3] = ModelConfig{Model: "kling-v3", CostPerSecond: map[string]float64{"9p": 1, "1p": 1, "5p": 1}}
	cfg.Images[ImagePurpose("bogus-b")] = ImageRoute{}
	cfg.Images[ImagePurpose("bogus-a")] = ImageRoute{}
	cfg.Policies[domain.QualityPolicy("bogus-b")] = PolicyRoute{}
	cfg.Policies[domain.QualityPolicy("bogus-a")] = PolicyRoute{}
	cfg.Routing.Weights[domain.QualityPolicy("bogus-b")] = RoutingWeights{}
	cfg.Routing.Weights[domain.QualityPolicy("bogus-a")] = RoutingWeights{}
	cfg.Routing.Candidates[domain.QualityPolicy("bogus-b")] = nil
	cfg.Routing.Candidates[domain.QualityPolicy("bogus-a")] = nil
	cfg.Routing.MinSamples = 0
	cfg.Retry.MaxAttempts = 0
	return cfg
}

func TestValidateErrorOrderIsDeterministic(t *testing.T) {
	cfg := brokenConfig()
	first := cfg.Validate()
	require.Error(t, first)

	for range 20 {
		require.Equal(t, first.Error(), cfg.Validate().Error())
	}
}

func TestValidateListsMapEntriesInKeyOrder(t *testing.T) {
	msg := brokenConfig().Validate().Error()

	for _, ordered := range [][]string{
		{`vendor "alpha" is unknown`, `vendor "mid" is unknown`, `vendor "zeta" is unknown`},
		{`models: "typo-a"`, `models: "typo-b"`},
		{`"1p" is not a resolution`, `"5p" is not a resolution`, `"9p" is not a resolution`},
		{`images: "bogus-a"`, `images: "bogus-b"`},
		{`policies: "bogus-a"`, `policies: "bogus-b"`},
		{`routing weights: "bogus-a"`, `routing weights: "bogus-b"`},
		{`routing candidates: "bogus-a"`, `routing candidates: "bogus-b"`},
	} {
		prev := -1
		for _, want := range ordered {
			i := strings.Index(msg, want)
			require.GreaterOrEqual(t, i, 0, "%q missing", want)
			assert.Greater(t, i, prev, "%q out of order", want)
			prev = i
		}
	}
}

func TestValidateStepsReportOnlyTheirOwnProblems(t *testing.T) {
	cfg := DefaultConfig()
	steps := map[string]func(Config) []error{
		"vendors": Config.validateVendors, "models": Config.validateModels,
		"images": Config.validateImageRoutes, "policies": Config.validatePolicies,
		"routing": Config.validateRouting, "retry": Config.validateRetry,
	}
	for name, step := range steps {
		assert.Empty(t, step(cfg), "%s: the default config is valid", name)
	}

	tests := []struct {
		name   string
		step   string
		mutate func(*Config)
		want   string
	}{
		{"vendor without base_url", "vendors", func(c *Config) { c.Vendors[VendorGoogle] = VendorConfig{} }, "base_url, max_concurrency and timeout_seconds must be set"},
		{"unknown model key", "models", func(c *Config) { c.Models["typo"] = ModelConfig{} }, `models: "typo" is not an image or video model`},
		{"missing image route", "images", func(c *Config) { delete(c.Images, PurposePreview) }, "images: no route for preview"},
		{"fast image without fast_model", "images", func(c *Config) {
			c.Images[PurposePreview] = ImageRoute{Primary: ImageTarget{Model: domain.ModelFlux2, Fast: true}, Fallback: c.Images[PurposePreview].Fallback}
		}, "has no fast_model"},
		{"missing policy route", "policies", func(c *Config) { delete(c.Policies, domain.QualityFast) }, "policies: no route for fast"},
		{"policy without takes", "policies", func(c *Config) {
			r := c.Policies[domain.QualityFast]
			r.Takes = 0
			c.Policies[domain.QualityFast] = r
		}, "policies fast: takes must be positive"},
		{"negative routing weight", "routing", func(c *Config) {
			c.Routing.Weights[domain.QualityFast] = RoutingWeights{Quality: -1, Cost: 2}
		}, "routing weights fast: must not be negative"},
		{"routing without min_samples", "routing", func(c *Config) { c.Routing.MinSamples = 0 }, "routing: min_samples must be positive"},
		{"retry max below base", "retry", func(c *Config) { c.Retry.MaxDelayMillis = c.Retry.BaseDelayMillis - 1 }, "max_delay_ms >= base_delay_ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(&cfg)

			errs := steps[tt.step](cfg)

			require.NotEmpty(t, errs)
			assert.Contains(t, errs[0].Error(), tt.want)
			for other, step := range steps {
				if other != tt.step {
					assert.Empty(t, step(cfg), "%s must not report a %s problem", other, tt.step)
				}
			}
		})
	}
}

func TestCheckTargets(t *testing.T) {
	cfg := DefaultConfig()
	delete(cfg.Vendors, VendorOf(domain.ModelFlux2))

	assert.Empty(t, cfg.checkImageTarget("where", ImageTarget{Model: domain.ModelImagen4, Fast: true}))
	assert.Contains(t, errStrings(cfg.checkImageTarget("where", ImageTarget{Model: domain.ModelKling3})), `where: "kling-3.0" is not a configured image model`)
	assert.Contains(t, errStrings(cfg.checkImageTarget("where", ImageTarget{Model: domain.ModelFlux2})), `where: no vendor settings for "flux"`)

	assert.Empty(t, cfg.checkVideoTarget("where", VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}))
	assert.Contains(t, errStrings(cfg.checkVideoTarget("where", VideoTarget{Model: domain.ModelKling3, Resolution: "480p"})), `where: "kling-3.0" does not support "480p"`)
	assert.Contains(t, errStrings(cfg.checkVideoTarget("where", VideoTarget{Model: domain.ModelName("veo-3"), Resolution: "720p"})), `where: "veo-3" is not a configured video model`)
}

func errStrings(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}
