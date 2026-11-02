package localcfg_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// A strength that is not a number (it would make the denoise NaN, which no JSON carries) is the default, like 0.
func TestStrengthOrDefault(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0, localcfg.DefaultReferenceStrength},
		{-1, localcfg.DefaultReferenceStrength},
		{math.NaN(), localcfg.DefaultReferenceStrength},
		{0.4, 0.4},
		{1, 1},
		{3, 1},
		{math.Inf(1), 1},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, localcfg.RefOption{Strength: c.in}.StrengthOrDefault(), "%v", c.in)
	}
}

// A face reference that names no strength stays longer than the whole picture does (#960); a strength that is named is the user's
// for every role, and the other roles keep the default they had.
func TestStrengthOrDefaultOfAFace(t *testing.T) {
	assert.Equal(t, localcfg.DefaultFaceReferenceStrength, localcfg.RefOption{Role: localcfg.RoleFace}.StrengthOrDefault())
	assert.Greater(t, localcfg.DefaultFaceReferenceStrength, localcfg.DefaultReferenceStrength)
	assert.Equal(t, 0.4, localcfg.RefOption{Role: localcfg.RoleFace, Strength: 0.4}.StrengthOrDefault(), "a named strength is not raised")
	for _, role := range []string{"", localcfg.RoleWhole, localcfg.RoleStyle, localcfg.RoleEdit} {
		assert.Equal(t, localcfg.DefaultReferenceStrength, localcfg.RefOption{Role: role}.StrengthOrDefault(), role)
	}
}
