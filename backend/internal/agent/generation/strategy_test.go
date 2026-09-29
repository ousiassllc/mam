package generation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

func ptr(f float64) *float64 { return &f }

func TestDecide_QualityPolicyPicksModelsResolutionAndTakeCount(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	cases := []struct {
		policy   domain.QualityPolicy
		primary  domain.ModelName
		fallback domain.ModelName
		res      string
		takes    int
	}{
		{domain.QualityFast, domain.ModelKling3, domain.ModelSeedance2, "720p", 2},
		{domain.QualityBalanced, domain.ModelSeedance2, domain.ModelKling3, "720p", 3},
		{domain.QualityMaxQuality, domain.ModelSeedance2, domain.ModelKling3, "1080p", 4},
	}
	for _, c := range cases {
		s, err := generation.Decide(cfg, c.policy, 5, repository.Budget{})
		require.NoError(t, err, c.policy)
		assert.Equal(t, c.primary, s.Primary.Model, c.policy)
		assert.Equal(t, c.fallback, s.Fallback.Model, c.policy)
		assert.Equal(t, c.res, s.Primary.Resolution, c.policy)
		assert.Equal(t, c.takes, s.Takes, c.policy)
		assert.Empty(t, s.Reasons, "nothing moved the plan away from the policy")
	}
}

func TestDecide_ClipLengthFollowsTheModelAndCostIsEstimatedForIt(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	s, err := generation.Decide(cfg, domain.QualityFast, 4, repository.Budget{}) // Kling: 5 s or 10 s clips
	require.NoError(t, err)
	assert.InDelta(t, 5, s.DurationSeconds, 1e-9)
	assert.InDelta(t, 5*0.07, s.CostPerTake, 1e-9)
}

func TestDecide_LongShotMovesToTheModelThatCanHoldIt(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	s, err := generation.Decide(cfg, domain.QualityFast, 12, repository.Budget{}) // Kling tops at 10 s, Seedance at 12 s
	require.NoError(t, err)
	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)
	assert.Equal(t, domain.ModelKling3, s.Fallback.Model)
	assert.InDelta(t, 12, s.DurationSeconds, 1e-9)
	require.Len(t, s.Reasons, 1)
	assert.Contains(t, s.Reasons[0], "longest clip")
}

func TestDecide_BudgetReducesTakesThenSwitchesModelThenFails(t *testing.T) {
	cfg := modeladapter.DefaultConfig()

	// Balanced, 4 s: Seedance 720p = 0.32 per Take, Kling 720p = 0.35 (5 s clip).
	s, err := generation.Decide(cfg, domain.QualityBalanced, 4, repository.Budget{ShotCeiling: ptr(0.7)})
	require.NoError(t, err)
	assert.Equal(t, 2, s.Takes, "0.7 pays for two Takes of 0.32")
	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)

	s, err = generation.Decide(cfg, domain.QualityBalanced, 4, repository.Budget{
		ProjectCeiling: ptr(10), ProjectSpent: 9.6, ShotCeiling: ptr(5), ShotSpent: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, s.Takes, "the tighter ceiling counts: 0.4 is left in the Project")

	_, err = generation.Decide(cfg, domain.QualityBalanced, 4, repository.Budget{ShotCeiling: ptr(0.1)})
	assert.ErrorIs(t, err, generation.ErrBudgetExceeded, "neither model fits")
}

func TestDecide_UsesTheCheaperAlternativeWhenOnlyItFits(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	// MAX QUALITY, 4 s: Seedance 1080p = 0.72 per Take; Kling 1080p (5 s clip) = 0.70.
	s, err := generation.Decide(cfg, domain.QualityMaxQuality, 4, repository.Budget{ShotCeiling: ptr(0.71)})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelKling3, s.Primary.Model)
	assert.Equal(t, 1, s.Takes)
	assert.Contains(t, s.Reasons[0], "does not fit the budget")
}
