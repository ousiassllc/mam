package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

func catalogVia(t *testing.T, p viaprovider.Provider, keys map[string]string) Catalog {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	cfg.VideoProvider = p
	store := credentialtest.New()
	for k, v := range keys {
		require.NoError(t, store.Set(k, v))
	}
	return Catalog{Adapter: modeladapter.New(cfg, localbackends.Build(cfg, store, localbackends.Options{}))}
}

func TestAvailabilitySaysWhyTheSelectedProviderDoesNotServeAModelAndWhereToFixIt(t *testing.T) {
	c := catalogVia(t, viaprovider.OpenRouter, map[string]string{"kling4": "k", "openrouter": "k"})

	av, ok := c.Availability(string(domain.ModelKling4))

	require.True(t, ok)
	assert.False(t, av.Available)
	assert.Equal(t, modelfilter.ReasonProvider, av.Reason)
	assert.Contains(t, av.Message, "OpenRouter は Kling 4.0 を提供していません")
	assert.Equal(t, "/settings/api-keys", av.Route, "the provider is chosen at the top of the API key category")
}

func TestAvailabilityNamesTheProvidersKeyWhenItIsMissing(t *testing.T) {
	c := catalogVia(t, viaprovider.Fal, map[string]string{"kling": "A:S", "kling4": "k"}) // the maker's keys do not serve fal.ai

	av, ok := c.Availability(string(domain.ModelKling3))

	require.True(t, ok)
	assert.False(t, av.Available)
	assert.Equal(t, modelfilter.ReasonNoKey, av.Reason)
	assert.Equal(t, "fal.ai の API キーが登録されていません（設定の「API キー」）", av.Message)
}

func TestAvailabilityIsUnchangedForTheOfficialProviderAndForOtherModels(t *testing.T) {
	official := catalogVia(t, viaprovider.Official, nil)
	av, _ := official.Availability(string(domain.ModelKling3))
	assert.Equal(t, NoKeyMessage, av.Message, "no aggregator: the generic message")

	viaFal := catalogVia(t, viaprovider.Fal, nil)
	av, _ = viaFal.Availability(string(domain.ModelSeedance2))
	assert.Equal(t, NoKeyMessage, av.Message, "Seedance is not served by an aggregator here")

	ready := catalogVia(t, viaprovider.Fal, map[string]string{"fal": "k"})
	av, _ = ready.Availability(string(domain.ModelKling4))
	assert.True(t, av.Available)
}
