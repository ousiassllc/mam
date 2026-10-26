package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// A Shot whose Quality Policy names an adult model while the age confirmation is off is refused up front, with a conflict
// that says how to go on (turn the age confirmation on, or choose another model): nothing is queued and no model is called.
func TestGenerateShot_RefusesAnAdultModelWhileTheAgeConfirmationIsOff(t *testing.T) {
	f := newGenFixtureWithDeps(t, true, func(string) (uint64, error) { return 1 << 40, nil }, func(d *service.Deps) {
		d.Models.Gate.Set(func(m domain.ModelName) bool { return m == domain.ModelSeedance2 }) // Balanced: the primary
	})

	_, err := f.svc.Generation.GenerateShot(context.Background(), f.shotID, "")

	var conflict *service.ConflictError
	require.ErrorAs(t, err, &conflict)
	assert.ErrorIs(t, err, service.ErrConflict)
	assert.Contains(t, conflict.Message, "seedance-2.0 は成人向けモデルで、年齢確認がオフのため使えません")
	assert.Contains(t, conflict.Message, "年齢確認をオンにするか")
	assert.Equal(t, domain.ShotPlanning, f.shot(t).State, "the Shot is not claimed")
	assert.Zero(t, f.count(t, `SELECT COUNT(*) FROM job`))
	assert.Empty(t, f.models.requests, "no model was called")
}
