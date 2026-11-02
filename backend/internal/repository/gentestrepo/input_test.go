package gentestrepo_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

func TestInputRoleAndStrengthRoundTrip(t *testing.T) {
	r, ctx := setup(t)
	nb := newBatch("b1", base, result("r1", 0, "m", "p"))
	origin := json.RawMessage(`{"kind":"upload"}`)
	nb.Inputs = []gentestrepo.Input{
		{ID: "i1", Role: gentestrepo.RoleFace, FilePath: "f1", MimeType: "image/png", Origin: origin, Position: 0, Strength: ptr(0.35)},
		{ID: "i2", Role: gentestrepo.RoleStyle, FilePath: "f2", MimeType: "image/png", Origin: origin, Position: 1, Strength: ptr(0.0)},
		{ID: "i3", Role: gentestrepo.RoleEdit, FilePath: "f3", MimeType: "image/png", Origin: origin, Position: 2},
		{ID: "i4", Role: gentestrepo.RoleReference, FilePath: "f4", MimeType: "image/png", Origin: origin, Position: 3, Strength: ptr(1.0)},
	}
	require.NoError(t, r.CreateBatch(ctx, nb))

	got, err := r.Inputs(ctx, "b1")
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, []string{gentestrepo.RoleFace, gentestrepo.RoleStyle, gentestrepo.RoleEdit, gentestrepo.RoleReference},
		[]string{got[0].Role, got[1].Role, got[2].Role, got[3].Role})
	require.NotNil(t, got[0].Strength)
	assert.InDelta(t, 0.35, *got[0].Strength, 1e-9)
	require.NotNil(t, got[1].Strength, "0 is a strength, not 'unset'")
	assert.Zero(t, *got[1].Strength)
	assert.Nil(t, got[2].Strength, "NULL is the default")
	require.NotNil(t, got[3].Strength)
	assert.InDelta(t, 1.0, *got[3].Strength, 1e-9)

	one, err := r.GetInput(ctx, "i1")
	require.NoError(t, err)
	require.NotNil(t, one.Strength)
	assert.InDelta(t, 0.35, *one.Strength, 1e-9)
}

func TestInputStrengthOutOfRangeIsRefusedByTheTable(t *testing.T) {
	r, ctx := setup(t)
	nb := newBatch("b1", base, result("r1", 0, "m", "p"))
	nb.Inputs = []gentestrepo.Input{{ID: "i1", Role: gentestrepo.RoleFace, FilePath: "f", MimeType: "image/png", Strength: ptr(1.5)}}
	assert.Error(t, r.CreateBatch(ctx, nb))
}
