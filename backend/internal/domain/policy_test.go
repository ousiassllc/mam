package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func intp(v int) *int           { return &v }
func floatp(v float64) *float64 { return &v }

func TestProjectPolicy_EffectiveUsesTheDefaultsOfTheQualityPolicy(t *testing.T) {
	fast := domain.ProjectPolicy{Quality: domain.QualityFast}.Effective()
	balanced := domain.ProjectPolicy{Quality: domain.QualityBalanced}.Effective()
	max := domain.ProjectPolicy{Quality: domain.QualityMaxQuality}.Effective()

	assert.Less(t, fast.MaxRetries, balanced.MaxRetries)
	assert.Less(t, balanced.MaxRetries, max.MaxRetries)
	assert.Less(t, fast.MaxGenerationTime, max.MaxGenerationTime)
	assert.Less(t, fast.MinAcceptanceScore, balanced.MinAcceptanceScore)
	assert.Less(t, balanced.MinAcceptanceScore, max.MinAcceptanceScore)
	assert.Less(t, fast.MinAxisScore, max.MinAxisScore)
	assert.Equal(t, domain.QualityFast, fast.Quality)
}

func TestProjectPolicy_EffectiveAppliesOverridesAndCeilings(t *testing.T) {
	p := domain.ProjectPolicy{
		Quality:              domain.QualityFast,
		ProjectBudgetCeiling: floatp(50), ShotBudgetCeiling: floatp(5),
		Limits: domain.PolicyLimits{MaxRetries: intp(0), MaxGenerationSeconds: intp(90), MinAcceptanceScore: intp(90)},
	}.Effective()

	assert.Equal(t, 0, p.MaxRetries, "an explicit 0 is an override, not 'unset'")
	assert.Equal(t, 90*time.Second, p.MaxGenerationTime)
	assert.Equal(t, 90, p.MinAcceptanceScore)
	assert.Equal(t, domain.ProjectPolicy{Quality: domain.QualityFast}.Effective().MinAxisScore, p.MinAxisScore, "an unset field keeps the default")
	assert.InDelta(t, 50, *p.ProjectBudgetCeiling, 1e-9)
	assert.InDelta(t, 5, *p.ShotBudgetCeiling, 1e-9)
}

func TestEffectivePolicy_CanRetryStopsAtMaxRetriesAndAtTheBudgets(t *testing.T) {
	p := domain.EffectivePolicy{MaxRetries: 2, ShotBudgetCeiling: floatp(3), ProjectBudgetCeiling: floatp(10)}

	assert.Equal(t, domain.RetryAllowed, p.CanRetry(0, 1, 1, 1))
	assert.Equal(t, domain.RetryAllowed, p.CanRetry(1, 2, 2, 1), "exactly at the Shot ceiling is allowed")
	assert.Equal(t, domain.RetryMaxRetries, p.CanRetry(2, 0, 0, 0), "Max retries per Shot")
	assert.Equal(t, domain.RetryShotBudget, p.CanRetry(1, 2.5, 2.5, 1), "Max cost per Shot")
	assert.Equal(t, domain.RetryProjectBudget, p.CanRetry(0, 0, 9.5, 1), "Project budget ceiling")

	noCeiling := domain.EffectivePolicy{MaxRetries: 1}
	assert.Equal(t, domain.RetryAllowed, noCeiling.CanRetry(0, 1e9, 1e9, 1e9), "no ceiling set")
	assert.Equal(t, domain.RetryMaxRetries, domain.EffectivePolicy{}.CanRetry(0, 0, 0, 0), "zero retries means none")
}

func TestEffectivePolicy_AcceptsNeedsTheOverallScoreAndEveryAxis(t *testing.T) {
	p := domain.EffectivePolicy{MinAcceptanceScore: 75, MinAxisScore: 50}
	ok := map[domain.EvaluationAxis]float64{domain.AxisStory: 90, domain.AxisActing: 60}

	assert.True(t, p.Accepts(80, ok))
	assert.True(t, p.Accepts(75, ok), "the threshold itself passes")
	assert.False(t, p.Accepts(74.9, ok), "overall below the acceptance score")
	assert.False(t, p.Accepts(90, map[domain.EvaluationAxis]float64{domain.AxisStory: 99, domain.AxisActing: 49}), "one weak axis fails a good average")
}

func TestQualityPolicy_ValidAndLabel(t *testing.T) {
	for _, q := range domain.QualityPolicies() {
		assert.True(t, q.Valid())
		assert.NotEqual(t, string(q), q.Label())
	}
	assert.False(t, domain.QualityPolicy("slow").Valid())
}
