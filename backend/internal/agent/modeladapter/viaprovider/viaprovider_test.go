package viaprovider_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestTheOfficialProviderHasNoRoutes(t *testing.T) {
	assert.Empty(t, viaprovider.Routes(viaprovider.Official))
	assert.Empty(t, viaprovider.Routes(""))
	_, ok := viaprovider.RouteOf("", domain.ModelKling3)
	assert.False(t, ok, "an unset provider is the official one")
	assert.Equal(t, viaprovider.Official, viaprovider.Provider("").Selected())
}

func TestEveryAggregatorServesKling3AndOnlyOpenRouterLacksKling4(t *testing.T) {
	for _, p := range []viaprovider.Provider{viaprovider.OpenRouter, viaprovider.Fal, viaprovider.Replicate} {
		k3, ok := viaprovider.RouteOf(p, domain.ModelKling3)
		require.True(t, ok)
		assert.True(t, k3.Supported(), "%s serves Kling 3.0", p)
		k4, ok := viaprovider.RouteOf(p, domain.ModelKling4)
		require.True(t, ok)
		assert.Equal(t, p != viaprovider.OpenRouter, k4.Supported(), "%s and Kling 4.0", p)
		if !k4.Supported() {
			assert.Contains(t, k4.Why, "OpenRouter は Kling 4.0 を提供していません")
			assert.Contains(t, k4.Why, "fal.ai", "it says where the model can be used")
			assert.Empty(t, k4.Variants)
		}
	}
}

func TestEverySupportedRouteHasAPricedVariantForEachResolutionOfItsCapabilities(t *testing.T) {
	for _, p := range []viaprovider.Provider{viaprovider.OpenRouter, viaprovider.Fal, viaprovider.Replicate} {
		for _, r := range viaprovider.Routes(p) {
			if !r.Supported() {
				continue
			}
			require.NotEmpty(t, r.Caps.Resolutions)
			for _, res := range r.Caps.Resolutions {
				v, ok := r.Variants[res]
				require.True(t, ok, "%s %s %s", p, r.Model, res)
				assert.NotEmpty(t, v.Endpoint)
				assert.Positive(t, v.USDPerSecond)
			}
		}
	}
}

func TestPerSecondIsThePriceOfTheProviderNotTheMakers(t *testing.T) {
	price, ok := viaprovider.PerSecond(viaprovider.Replicate, domain.ModelKling3, "1080p")
	assert.True(t, ok)
	assert.InDelta(t, 0.224, price, 1e-9)
	price, ok = viaprovider.PerSecond(viaprovider.OpenRouter, domain.ModelKling3, "1080p")
	assert.True(t, ok)
	assert.InDelta(t, 0.112, price, 1e-9)

	_, ok = viaprovider.PerSecond(viaprovider.OpenRouter, domain.ModelKling4, "720p")
	assert.False(t, ok, "a model the provider does not serve has no price there")
	_, ok = viaprovider.PerSecond(viaprovider.Official, domain.ModelKling3, "720p")
	assert.False(t, ok)
	_, ok = viaprovider.PerSecond(viaprovider.Fal, domain.ModelSeedance2, "720p")
	assert.False(t, ok, "a model the setting does not concern")
}

func TestOnlyTheAggregatorsHaveAKeyVendor(t *testing.T) {
	assert.Empty(t, viaprovider.Official.Vendor())
	assert.Equal(t, "openrouter", viaprovider.OpenRouter.Vendor())
	assert.Equal(t, "fal", viaprovider.Fal.Vendor())
	assert.Equal(t, "replicate", viaprovider.Replicate.Vendor())
	assert.False(t, viaprovider.Provider("x").Valid())
}
