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
