// Package qualitytest checks the quality buttons (速い / 標準 / きれい, Issue #977) and the intents (人物写真 / アニメ / 風景 / 同じ顔で) of the
// generation test from the request to the graph the model is sent and to what a result keeps.
package qualitytest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

var ctx = context.Background

func ptr[T any](v T) *T { return &v }

func rig(t *testing.T, opts ...gentesttest.Option) *gentesttest.Rig {
	t.Helper()
	return gentesttest.New(t, append([]gentesttest.Option{gentesttest.WithInstalled(map[string]string{
		"sdxl-base": "txt2img-sdxl", "flux1-schnell": "txt2img-flux-schnell",
	})}, opts...)...)
}

func request(q sampling.Quality, models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}, Seed: ptr(int64(7)), Quality: q}
}

// call is what the backend was asked for the one model: the steps and cfg of its sampling.
func ranWith(t *testing.T, r *gentesttest.Rig, req gentest.Request) sampling.Settings {
	t.Helper()
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Len(t, st.Results, 1)
	require.Equal(t, gentest.StateSucceeded, st.Results[0].Status, st.Results[0].Error)
	require.NotNil(t, st.Results[0].Sampling)
	return *st.Results[0].Sampling
}

// Nothing touched, and the standard quality, are the template as it ships: the same steps and cfg, nothing overridden.
func TestStandardIsTheModelAsItShipsAndFastAndHighChangeTheSteps(t *testing.T) {
	id := gentesttest.InstalledID("sdxl-base")
	r := rig(t)
	tpl, _ := localcfg.TemplateByID("txt2img-sdxl")
	own := tpl.Model.Workflow.SamplingCaps()
	require.NotNil(t, own.Presets, "the SDXL template ships quality presets")

	plain := ranWith(t, r, request("", id))
	assert.Equal(t, sampling.Settings{Steps: ptr(25), CFG: ptr(7.0), Sampler: "dpmpp_2m", Scheduler: "karras"}, plain)
	assert.Equal(t, plain, ranWith(t, r, request(sampling.QualityStandard, id)), "standard changes nothing")

	fast := ranWith(t, r, request(sampling.QualityFast, id))
	high := ranWith(t, r, request(sampling.QualityHigh, id))
	assert.Equal(t, *own.Presets.Fast.Steps, *fast.Steps)
	assert.Equal(t, *own.Presets.High.Steps, *high.Steps)
	assert.Less(t, *fast.Steps, 25)
	assert.Greater(t, *high.Steps, 25)
	assert.Equal(t, 7.0, *fast.CFG, "a preset that names no cfg keeps the model's own")
	assert.Equal(t, "dpmpp_2m", fast.Sampler)
}

// What the user typed wins over the preset: the "こだわり" screen starts from a quality and changes an item.
func TestAValueTheUserNamedWinsOverThePreset(t *testing.T) {
	id := gentesttest.InstalledID("sdxl-base")
	r := rig(t)
	req := request(sampling.QualityFast, id)
	req.Sampling = sampling.Settings{Steps: ptr(33), Sampler: "heun"}
	got := ranWith(t, r, req)
	assert.Equal(t, 33, *got.Steps)
	assert.Equal(t, "heun", got.Sampler)
}

// A model without presets is made at its own values and the estimate says so; a quality that is not one is refused.
func TestAModelWithoutPresetsWarnsAndAQualityThatIsNotOneIsRefused(t *testing.T) {
	r := rig(t)
	schnell := gentesttest.InstalledID("flux1-schnell")
	est, err := r.Svc.Estimate(ctx(), request(sampling.QualityHigh, schnell))
	require.NoError(t, err)
	var codes []string
	for _, w := range est.Warnings {
		codes = append(codes, w.Code)
	}
	assert.Contains(t, codes, gentest.WarnQualityIgnored)
	assert.Equal(t, 4, *ranWith(t, r, request(sampling.QualityHigh, schnell)).Steps)

	est, err = r.Svc.Estimate(ctx(), request(sampling.QualityHigh, gentesttest.InstalledID("sdxl-base")))
	require.NoError(t, err)
	for _, w := range est.Warnings {
		assert.NotEqual(t, gentest.WarnQualityIgnored, w.Code)
	}

	var invalid *gterr.InvalidError
	_, err = r.Svc.Estimate(ctx(), request("ultra", gentesttest.InstalledID("sdxl-base")))
	assert.ErrorAs(t, err, &invalid)
}
