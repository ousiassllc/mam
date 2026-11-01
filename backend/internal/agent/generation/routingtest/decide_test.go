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
		{domain.QualityMaxQuality, domain.ModelRunwayGen45, domain.ModelKling3, "720p", 4},
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
	assert.InDelta(t, 5*0.084, s.CostPerTake, 1e-9)
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

	// Balanced, 4 s: Seedance 720p = 0.60 per Take, Kling 720p = 0.42 (5 s clip).
	s, err := generation.Decide(cfg, domain.QualityBalanced, 4, repository.Budget{ShotCeiling: ptr(1.3)})
	require.NoError(t, err)
	assert.Equal(t, 2, s.Takes, "1.3 pays for two Takes of 0.60")
	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)

	s, err = generation.Decide(cfg, domain.QualityBalanced, 4, repository.Budget{
		ProjectCeiling: ptr(10), ProjectSpent: 9.3, ShotCeiling: ptr(5), ShotSpent: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, s.Takes, "the tighter ceiling counts: 0.7 is left in the Project")

	_, err = generation.Decide(cfg, domain.QualityBalanced, 4, repository.Budget{ShotCeiling: ptr(0.1)})
	assert.ErrorIs(t, err, generation.ErrBudgetExceeded, "neither model fits")
}

func TestDecide_UsesTheCheaperAlternativeWhenOnlyItFits(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	// FAST, 4 s: Kling 720p (5 s clip) = 0.42 per Take; Seedance 480p = 0.28.
	s, err := generation.Decide(cfg, domain.QualityFast, 4, repository.Budget{ShotCeiling: ptr(0.3)})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelSeedance2, s.Primary.Model)
	assert.Equal(t, 1, s.Takes)
	assert.Contains(t, s.Reasons[0], "does not fit the budget")
}

func TestDecide_MaxQualityUsesRunwayAndTheDifferentModelIsKling(t *testing.T) {
	s, err := generation.Decide(modeladapter.DefaultConfig(), domain.QualityMaxQuality, 4, repository.Budget{})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelRunwayGen45, s.Primary.Model)
	assert.Equal(t, domain.ModelKling3, s.Fallback.Model)
	assert.InDelta(t, 4, s.DurationSeconds, 1e-9, "Runway makes clips of any whole second from 2 to 10")
	assert.InDelta(t, 4*0.12, s.CostPerTake, 1e-9)
}

func TestDecideWithModel_TheModelTheUserFixedIsTheOnlyOne(t *testing.T) {
	cfg := modeladapter.DefaultConfig()

	s, err := generation.DecideWithModel(cfg, domain.QualityBalanced, 4, repository.Budget{}, domain.ModelKling3)
	require.NoError(t, err)
	assert.Equal(t, domain.ModelKling3, s.Primary.Model)
	assert.Equal(t, domain.ModelKling3, s.Fallback.Model, "no other model is called instead")
	assert.Equal(t, "720p", s.Primary.Resolution, "the Quality Policy still sets the resolution")
	assert.Equal(t, 3, s.Takes)

	long, err := generation.DecideWithModel(cfg, domain.QualityFast, 12, repository.Budget{}, domain.ModelKling3)
	require.NoError(t, err)
	assert.Equal(t, domain.ModelKling3, long.Primary.Model, "a longer Shot does not swap the fixed model")

	_, err = generation.DecideWithModel(cfg, domain.QualityBalanced, 4, repository.Budget{ShotCeiling: ptr(0.1)}, domain.ModelKling3)
	assert.ErrorIs(t, err, generation.ErrBudgetExceeded, "and it is not replaced by a cheaper one")

	auto, err := generation.DecideWithModel(cfg, domain.QualityBalanced, 4, repository.Budget{}, "")
	require.NoError(t, err)
	same, _ := generation.Decide(cfg, domain.QualityBalanced, 4, repository.Budget{})
	assert.Equal(t, same, auto)
}
