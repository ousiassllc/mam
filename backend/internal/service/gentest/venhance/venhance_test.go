package venhance_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

var wan = cloudenhance.Source{Width: 480, Height: 848, FPS: 16, FrameCount: 81, Duration: 5.0625}

func TestResolveUpscale(t *testing.T) {
	p, err := venhance.Resolve(wan, cloudenhance.Params{Op: cloudenhance.OpUpscale, TargetShortSide: 720}, true, 0)
	require.NoError(t, err)
	assert.Equal(t, [2]int{720, 1272}, [2]int{p.Width, p.Height}, "480 × 848 at 720 ends 720 × 1272")
	assert.InDelta(t, 1.5, p.Scale, 1e-9)
	assert.InDelta(t, 16, p.FPS, 0)

	p, err = venhance.Resolve(wan, cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 2}, true, 0)
	require.NoError(t, err)
	assert.Equal(t, [2]int{960, 1696}, [2]int{p.Width, p.Height})

	odd := cloudenhance.Source{Width: 853, Height: 481, FPS: 24}
	p, err = venhance.Resolve(odd, cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 2}, true, 0)
	require.NoError(t, err)
	assert.Equal(t, [2]int{1706, 962}, [2]int{p.Width, p.Height}, "even sides")
}

func TestResolveRefusesWhatCannotBeMade(t *testing.T) {
	for name, tc := range map[string]struct {
		p     cloudenhance.Params
		local bool
		max   int
		want  string
	}{
		"smaller short side":  {cloudenhance.Params{Op: cloudenhance.OpUpscale, TargetShortSide: 360}, true, 0, "元より大きく"},
		"over four times":     {cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 4.5}, true, 0, "倍率"},
		"nothing asked":       {cloudenhance.Params{Op: cloudenhance.OpUpscale}, true, 0, "target_short_side"},
		"both":                {cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 2, TargetShortSide: 720}, true, 0, "どちらか一方"},
		"pixel limit":         {cloudenhance.Params{Op: cloudenhance.OpUpscale, Scale: 4}, true, 2073600, "上限"},
		"interpolate values":  {cloudenhance.Params{Op: cloudenhance.OpInterpolate, Scale: 2}, true, 0, "scale"},
		"fps not a multiple":  {cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 24}, true, 0, "整数倍"},
		"fps not a multiple2": {cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 48}, true, 0, "整数倍"},
		"fps not larger":      {cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 10}, false, 0, "より大きく"},
		"cloud fps too high":  {cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 300}, false, 0, "240"},
		"multiplier":          {cloudenhance.Params{Op: cloudenhance.OpInterpolate, Multiplier: 3}, true, 0, "2 か 4"},
		"op":                  {cloudenhance.Params{Op: "x"}, true, 0, "op"},
	} {
		_, err := venhance.Resolve(wan, tc.p, tc.local, tc.max)
		var inv *gterr.InvalidError
		require.ErrorAs(t, err, &inv, name)
		assert.Contains(t, inv.Message, tc.want, name)
	}
}

func TestResolveInterpolate(t *testing.T) {
	p, err := venhance.Resolve(wan, cloudenhance.Params{Op: cloudenhance.OpInterpolate, Multiplier: 2}, true, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, p.Multiplier)
	assert.InDelta(t, 32, p.FPS, 0)

	p, err = venhance.Resolve(wan, cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 64}, true, 0)
	require.NoError(t, err)
	assert.Equal(t, 4, p.Multiplier, "64 is 16 × 4")

	ntsc := cloudenhance.Source{Width: 640, Height: 360, FPS: 23.976}
	p, err = venhance.Resolve(ntsc, cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 47.952}, true, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, p.Multiplier, "within 0.01 fps of a multiple")

	p, err = venhance.Resolve(wan, cloudenhance.Params{Op: cloudenhance.OpInterpolate, TargetFPS: 24}, false, 0)
	require.NoError(t, err, "the cloud takes any rate in its range")
	assert.Zero(t, p.Multiplier)
	assert.InDelta(t, 24, p.FPS, 0)
}

func TestEstimatesExpireAndRunOnce(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	e := venhance.NewEstimates(func() time.Time { return now })
	id, expires := e.Put(venhance.Quote{ResultID: "r1"})
	assert.Equal(t, now.Add(venhance.EstimateTTL), expires)

	q, ok := e.Get(id)
	require.True(t, ok)
	assert.Equal(t, "r1", q.ResultID)
	_, ok = e.Take(id)
	assert.True(t, ok)
	_, ok = e.Take(id)
	assert.False(t, ok, "a quote runs once")

	id2, _ := e.Put(venhance.Quote{})
	now = now.Add(venhance.EstimateTTL)
	_, ok = e.Get(id2)
	assert.False(t, ok, "over after the TTL")
	id3, _ := e.Put(venhance.Quote{})
	assert.NotEqual(t, id2, id3, "the ids are not guessable counters")
}
