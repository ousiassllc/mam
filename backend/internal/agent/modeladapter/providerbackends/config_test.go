package providerbackends_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func configVia(p viaprovider.Provider) modeladapter.Config {
	cfg := modeladapter.DefaultConfig()
	cfg.VideoProvider = p
	return cfg
}

func TestEstimatesUseTheUnitPriceOfTheSelectedProvider(t *testing.T) {
	t720 := modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}
	t1080 := modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "1080p"}
	for name, tc := range map[string]struct {
		provider viaprovider.Provider
		want720  float64
		want1080 float64
	}{
		"official":   {viaprovider.Official, 0.084, 0.112},
		"unset":      {"", 0.084, 0.112},
		"openrouter": {viaprovider.OpenRouter, 0.084, 0.112},
		"fal":        {viaprovider.Fal, 0.084, 0.112},
		"replicate":  {viaprovider.Replicate, 0.168, 0.224},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := configVia(tc.provider)
			assert.InDelta(t, tc.want720, cfg.VideoPerSecond(t720), 1e-9)
			assert.InDelta(t, tc.want1080, cfg.VideoPerSecond(t1080), 1e-9)
			assert.InDelta(t, tc.want720*7, cfg.VideoCost(t720, 7), 1e-9)
		})
	}
	k4 := modeladapter.VideoTarget{Model: domain.ModelKling4, Resolution: "1080p"}
	assert.InDelta(t, 0.21, configVia(viaprovider.Official).VideoPerSecond(k4), 1e-9)
	assert.InDelta(t, 0.235, configVia(viaprovider.Replicate).VideoPerSecond(k4), 1e-9)
	assert.InDelta(t, 0.235, configVia(viaprovider.Fal).VideoCost(k4, 1), 1e-9)
	assert.InDelta(t, 0.21, configVia(viaprovider.OpenRouter).VideoPerSecond(k4), 1e-9,
		"OpenRouter does not serve Kling 4.0: the official price is what is left to show, next to the reason")
	other := modeladapter.VideoTarget{Model: domain.ModelSeedance2, Resolution: "720p"}
	assert.InDelta(t, 0.15, configVia(viaprovider.Fal).VideoPerSecond(other), 1e-9, "a model of no aggregator keeps its price")
}

func TestCapabilitiesFollowTheProviderForKlingOnly(t *testing.T) {
	official, ok := configVia(viaprovider.Official).VideoCaps(domain.ModelKling3)
	require.True(t, ok)
	assert.Equal(t, []float64{5, 10}, official.Durations)
	via, ok := configVia(viaprovider.Fal).VideoCaps(domain.ModelKling3)
	require.True(t, ok)
	assert.Equal(t, 3.0, via.Durations[0])
	assert.Equal(t, 15.0, via.Durations[len(via.Durations)-1])
	assert.True(t, via.FirstFrame && via.LastFrame)

	// Not served: the capabilities stay the official ones, so that a route that names the model is still checked as before.
	k4, ok := configVia(viaprovider.OpenRouter).VideoCaps(domain.ModelKling4)
	require.True(t, ok)
	assert.Equal(t, 30.0, k4.Durations[len(k4.Durations)-1])

	seed, _ := configVia(viaprovider.Replicate).VideoCaps(domain.ModelSeedance2)
	assert.Equal(t, 12.0, seed.Durations[len(seed.Durations)-1])
}

func TestPrepareSnapsToTheLengthsOfTheProvider(t *testing.T) {
	job := modeladapter.VideoJob{Spec: domain.ShotSpec{ShotObjective: "A fox runs"}, DurationSeconds: 7}
	target := modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}

	official, err := configVia(viaprovider.Official).Prepare(target, job)
	require.NoError(t, err)
	assert.Equal(t, 10.0, official.DurationSeconds, "the official API takes 5 or 10 seconds")
	via, err := configVia(viaprovider.OpenRouter).Prepare(target, job)
	require.NoError(t, err)
	assert.Equal(t, 7.0, via.DurationSeconds)
}

func TestUnavailableSaysWhyAProviderCannotServeAModel(t *testing.T) {
	cfg := configVia(viaprovider.OpenRouter)
	a := modeladapter.New(cfg, modeladapter.NewBackends(cfg, credentialtest.New(), nil, 0))

	assert.Equal(t, domain.ExcludedProvider, a.Unavailable(domain.ModelKling4), "whatever keys are stored")
	assert.Contains(t, cfg.ProviderRefusal(domain.ModelKling4), "OpenRouter")
	assert.Empty(t, cfg.ProviderRefusal(domain.ModelKling3))
	assert.Empty(t, cfg.ProviderRefusal(domain.ModelSeedance2))
	assert.Empty(t, configVia(viaprovider.Official).ProviderRefusal(domain.ModelKling4))
}

func TestTheVendorOfAModelIsTheAggregatorsWhereItServesIt(t *testing.T) {
	assert.Equal(t, modeladapter.VendorKling, configVia(viaprovider.Official).VendorOf(domain.ModelKling3))
	assert.Equal(t, modeladapter.VendorFal, configVia(viaprovider.Fal).VendorOf(domain.ModelKling3))
	assert.Equal(t, modeladapter.VendorKling4, configVia(viaprovider.OpenRouter).VendorOf(domain.ModelKling4), "not served: the breaker of the maker's vendor")
	assert.Equal(t, modeladapter.VendorSeedance, configVia(viaprovider.Replicate).VendorOf(domain.ModelSeedance2))
	assert.Equal(t, "Replicate", viaprovider.KeyLabel(viaprovider.Replicate, domain.ModelKling3))
	assert.Empty(t, viaprovider.KeyLabel(viaprovider.Replicate, domain.ModelSeedance2))
	assert.Empty(t, viaprovider.KeyLabel(viaprovider.OpenRouter, domain.ModelKling4))
}
