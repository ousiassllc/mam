package routingtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// A Luma Ray 3.2 clip with a frame is 5 seconds long (Prepare), so the estimate of a 10 second Shot with a Keyframe is one 5
// second clip, not three: the budget then holds more Takes. Without a frame the clip is 10 seconds and costs three times that.
func TestDecideRouted_LumaWithAKeyframeIsEstimatedAsA5SecondClip(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	ceiling := 0.9
	budget := repository.Budget{ShotCeiling: &ceiling}
	withFrame := generation.RoutingInput{References: []domain.Reference{{Kind: domain.RefKeyframe}}}
	otherRef := generation.RoutingInput{References: []domain.Reference{{Kind: domain.RefCharacter}}}

	framed, err := generation.DecideRouted(cfg, domain.QualityBalanced, 10, budget, domain.ModelLumaRay32, &withFrame)
	require.NoError(t, err)
	assert.Equal(t, 5.0, framed.DurationSeconds)
	assert.InDelta(t, 0.3, framed.CostPerTake, 1e-9, "720p: 100 credits")
	assert.Equal(t, 3, framed.Takes, "three 5 second Takes fit 0.90 USD")

	for name, in := range map[string]*generation.RoutingInput{"no references": nil, "no Keyframe": &otherRef} {
		plain, err := generation.DecideRouted(cfg, domain.QualityBalanced, 10, budget, domain.ModelLumaRay32, in)
		require.NoError(t, err, name)
		assert.Equal(t, 10.0, plain.DurationSeconds, name)
		assert.InDelta(t, 0.9, plain.CostPerTake, 1e-9, "%s: 300 credits", name)
		assert.Equal(t, 1, plain.Takes, "%s: one 10 second Take fits 0.90 USD", name)
	}

	other, err := generation.DecideRouted(cfg, domain.QualityBalanced, 10, budget, domain.ModelKling3, &withFrame)
	require.NoError(t, err)
	assert.Equal(t, 10.0, other.DurationSeconds, "a frame does not shorten the clip of another model")
}
