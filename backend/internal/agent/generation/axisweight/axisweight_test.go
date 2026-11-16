package axisweight_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/generation/axisweight"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestOf_WeightsSumToOneAndTheNatureOfTheShotRaisesItsAxes(t *testing.T) {
	plain := axisweight.Of(nil)
	assert.InDelta(t, 0.25, plain.Product, 1e-9, "a Shot that depends on nothing weighs the axes alike")

	spec := &domain.ShotSpec{Product: &domain.SpecProduct{}}
	spec.Camera.Movement = "dolly"
	w := axisweight.Of(spec)
	assert.InDelta(t, 1, w.Product+w.Acting+w.Camera+w.Continuity, 1e-9)
	assert.Greater(t, w.Product, w.Acting)
	assert.Greater(t, w.Camera, w.Continuity)
	assert.InDelta(t, w.Product, w.Camera, 1e-9)

	static := &domain.ShotSpec{}
	static.Camera.Movement = " Static "
	assert.InDelta(t, 0.25, axisweight.Of(static).Camera, 1e-9, "a static camera is no camera movement")
}

func TestScore_IsTheWeightedMeanOfTheAxes(t *testing.T) {
	scores := domain.TakeScores{Product: 80, Acting: 60, Camera: 40, Continuity: 20}
	assert.InDelta(t, 50, axisweight.Score(scores, axisweight.Of(nil)), 1e-9)
}
