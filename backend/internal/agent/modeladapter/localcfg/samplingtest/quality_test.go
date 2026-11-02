package samplingtest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
)

func ptr[T any](v T) *T { return &v }

// The templates that ship quality presets keep them where the buttons can use them: fast below the template's own steps, high above, and a
// distilled model (few steps, no room to go lower) or a speed-mode model has none: the screen then says why there are no buttons.
func TestShippedPresetsLieOnEachSideOfTheTemplatesOwnSteps(t *testing.T) {
	withPresets := 0
	for _, tpl := range localcfg.Templates() {
		w := tpl.Model.Workflow
		if w == nil || w.QualityPresets == nil {
			continue
		}
		withPresets++
		caps := w.SamplingCaps()
		require.NotNil(t, caps, tpl.ID)
		require.NotNil(t, caps.Presets, tpl.ID+": the caps hand the presets to the screen")
		assert.Less(t, *w.QualityPresets.Fast.Steps, int(caps.Steps.Default), tpl.ID)
		assert.Greater(t, *w.QualityPresets.High.Steps, int(caps.Steps.Default), tpl.ID)
		assert.Empty(t, localcfg.ValidateModel("test/"+tpl.ID, tpl.Model, map[string]localcfg.Endpoint{"test": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}), tpl.ID)
	}
	assert.Positive(t, withPresets)
	for _, id := range []string{"txt2img-flux-schnell", "txt2img-flux2-klein"} {
		assert.Nil(t, template(t, id).Workflow.SamplingCaps().Presets, id+": 4 steps are what it is made for")
	}
}

// A preset that cannot do what it says is refused when the workflow is loaded.
func TestABadPresetIsRefused(t *testing.T) {
	endpoints := map[string]localcfg.Endpoint{"test": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
	problems := func(edit func(*localcfg.Model)) string {
		m := template(t, "txt2img-sdxl")
		w := *m.Workflow
		m.Workflow = &w
		edit(&m)
		var out []string
		for _, err := range localcfg.ValidateModel("test/sdxl", m, endpoints) {
			out = append(out, err.Error())
		}
		return strings.Join(out, "\n")
	}
	cases := map[string]struct {
		edit func(*localcfg.Model)
		want string
	}{
		"empty": {func(m *localcfg.Model) { m.Workflow.QualityPresets = &sampling.Presets{} }, "names neither fast nor high"},
		"fast above": {func(m *localcfg.Model) {
			m.Workflow.QualityPresets = &sampling.Presets{Fast: &sampling.Preset{Steps: ptr(30)}}
		}, "must be lower"},
		"high below": {func(m *localcfg.Model) {
			m.Workflow.QualityPresets = &sampling.Presets{High: &sampling.Preset{Steps: ptr(10)}}
		}, "must be higher"},
		"out of range": {func(m *localcfg.Model) {
			m.Workflow.QualityPresets = &sampling.Presets{High: &sampling.Preset{Steps: ptr(500)}}
		}, "ステップ数は"},
		"nothing in it": {func(m *localcfg.Model) { m.Workflow.QualityPresets = &sampling.Presets{Fast: &sampling.Preset{}} }, "names neither steps nor cfg"},
		"speed modes own": {func(m *localcfg.Model) {
			m.Workflow.QualityPresets = &sampling.Presets{Fast: &sampling.Preset{Steps: ptr(10)}}
			m.Workflow.SpeedModes = []localcfg.SpeedMode{{ID: "a"}}
		}, "speed_modes"},
		"no steps in graph": {func(m *localcfg.Model) {
			m.Workflow.QualityPresets = &sampling.Presets{Fast: &sampling.Preset{Steps: ptr(10)}}
			var kept []localcfg.Assignment
			for _, a := range m.Workflow.Assignments {
				if a.Param != localcfg.ParamSteps {
					kept = append(kept, a)
				}
			}
			m.Workflow.Assignments = kept
		}, "no steps assignment"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, problems(c.edit), c.want, fmt.Sprint(name))
		})
	}
	assert.NotContains(t, problems(func(*localcfg.Model) {}), "quality_presets")
}

// 標準は何も上書きしない。速い・きれいはプリセットの値、利用者が書いた値はプリセットに勝つ。速さのモードのあるモデルは品質で動かない。
func TestWithQualityOnlyFillsWhatTheUserLeftOut(t *testing.T) {
	caps := template(t, "txt2img-sdxl").Workflow.SamplingCaps()
	fast, high := caps.Presets.Fast, caps.Presets.High
	assert.Equal(t, sampling.Settings{}, caps.WithQuality(sampling.QualityStandard, sampling.Settings{}))
	assert.Equal(t, sampling.Settings{}, caps.WithQuality("", sampling.Settings{}))
	assert.Equal(t, sampling.Settings{Steps: fast.Steps}, caps.WithQuality(sampling.QualityFast, sampling.Settings{}))
	assert.Equal(t, sampling.Settings{Steps: high.Steps, Sampler: "heun"}, caps.WithQuality(sampling.QualityHigh, sampling.Settings{Sampler: "heun"}))
	assert.Equal(t, sampling.Settings{Steps: ptr(33)}, caps.WithQuality(sampling.QualityFast, sampling.Settings{Steps: ptr(33)}))

	var none *sampling.Caps
	assert.Equal(t, sampling.Settings{}, none.WithQuality(sampling.QualityHigh, sampling.Settings{}))
	wan := template(t, "img2video-wan22-14b-lightx2v").Workflow.SamplingCaps()
	assert.Equal(t, sampling.Settings{}, wan.WithQuality(sampling.QualityHigh, sampling.Settings{}), "the speed mode owns the steps")
}

func TestParseQuality(t *testing.T) {
	for in, want := range map[string]sampling.Quality{"": sampling.QualityStandard, "fast": sampling.QualityFast, "standard": sampling.QualityStandard, "high": sampling.QualityHigh} {
		got, err := sampling.ParseQuality(in)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := sampling.ParseQuality("きれい")
	assert.Error(t, err)
	assert.Equal(t, "速い", sampling.QualityFast.Label())
	assert.Equal(t, "標準", sampling.QualityStandard.Label())
	assert.Equal(t, "きれい", sampling.QualityHigh.Label())
}
