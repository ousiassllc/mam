package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestRatio_AMetricWithoutADenominatorIsNotDefinedInsteadOfZero(t *testing.T) {
	assert.False(t, domain.Ratio{Numerator: 3}.Defined())
	assert.Zero(t, domain.Ratio{Numerator: 3}.Value())
	r := domain.Ratio{Numerator: 1, Denominator: 4}
	assert.True(t, r.Defined())
	assert.InDelta(t, 0.25, r.Value(), 1e-9)
}

func TestOverridesAIPick_OnlyAChoiceOfAnotherTakeContradictsTheAI(t *testing.T) {
	take := func(id string, status domain.TakeStatus) domain.TakeInfo {
		return domain.TakeInfo{Take: domain.Take{ID: id, Status: status}}
	}
	takes := []domain.TakeInfo{take("a", domain.TakeAIPick), take("b", domain.TakeCandidate)}
	assert.True(t, domain.OverridesAIPick(takes, "b"))
	assert.False(t, domain.OverridesAIPick(takes, "a"), "choosing the AI Pick agrees with it")
	assert.False(t, domain.OverridesAIPick([]domain.TakeInfo{take("a", domain.TakeCandidate), take("b", domain.TakeCandidate)}, "b"), "no AI Pick, nothing to override")
}
