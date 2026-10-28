package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/domain"
	gentestrepo "github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// A model the age gate refuses is never listed (the lists leave it out); were it listed, the reason must say the gate, not a
// missing API key or an unreachable endpoint, whatever the source of the model.
func TestUnavailableMessageNamesTheAgeGateWhateverTheSource(t *testing.T) {
	for _, source := range []string{SourceLocal, SourceWorkflow, SourceChatGPT, ""} {
		assert.Contains(t, unavailableMessage(agegate.Excluded, source), "年齢確認がオフ", source)
	}
}

// Luma bills a clip of up to 5 seconds as a 5 second one: the catalog shows the price of a second of that clip (the price in
// models.json), not the cost of a 1 second clip, which would be five times as much.
func TestPriceOfLumaIsThePriceOfASecondOfA5SecondClip(t *testing.T) {
	p := priceOf(modeladapter.DefaultConfig(), domain.ModelLumaRay32, gentestrepo.MediaVideo)

	assert.Equal(t, BasisPriced, p.Basis)
	assert.InDelta(t, 0.012, p.PerSecondUSD["360p"], 1e-9)
	assert.InDelta(t, 0.03, p.PerSecondUSD["540p"], 1e-9)
	assert.InDelta(t, 0.06, p.PerSecondUSD["720p"], 1e-9)
	assert.InDelta(t, 0.24, p.PerSecondUSD["1080p"], 1e-9)
}

// The other vendors' per second price is unchanged: the price of a clip of one second.
func TestPriceOfKlingIsThePriceOfOneSecond(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	p := priceOf(cfg, domain.ModelKling3, gentestrepo.MediaVideo)

	require.NotEmpty(t, p.PerSecondUSD)
	for res, perSecond := range p.PerSecondUSD {
		assert.InDelta(t, cfg.VideoCost(modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: res}, 1), perSecond, 1e-9, res)
	}
}
