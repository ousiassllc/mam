package samplefile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/samplefile"
)

func templateModel(t *testing.T, id string) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return tpl.Model
}

// The values of a model's own recommendation replace the template's literals (where the caps read the defaults from), and the template is not touched.
func TestModelPutsTheRecommendedValuesWhereTheDefaultsAreRead(t *testing.T) {
	base := templateModel(t, "txt2img-sdxl")
	before := base.Workflow.SamplingCaps()
	require.Equal(t, 25.0, before.Steps.Default)

	steps, cfg := 30, 5.5
	m, err := samplefile.Model(base, localcfg.SamplingSettings{Steps: &steps, CFG: &cfg, Sampler: "euler_ancestral"})
	require.NoError(t, err)
	caps := m.Workflow.SamplingCaps()
	assert.Equal(t, 30.0, caps.Steps.Default)
	assert.Equal(t, 5.5, caps.CFG.Default)
	assert.Equal(t, "euler_ancestral", caps.Sampler.Default)
	assert.Equal(t, "karras", caps.Scheduler.Default, "what is not named stays the template's")
	assert.Equal(t, before, base.Workflow.SamplingCaps(), "the template is not changed")

	same, err := samplefile.Model(base, localcfg.SamplingSettings{})
	require.NoError(t, err)
	assert.JSONEq(t, string(base.Workflow.Graph), string(same.Workflow.Graph), "nothing named, nothing changed")
}

func TestModelRefusesWhatTheWorkflowCannotTake(t *testing.T) {
	steps, tooMany := 8, 500
	for name, tc := range map[string]struct {
		model localcfg.Model
		s     localcfg.SamplingSettings
	}{
		"out of range":                   {templateModel(t, "txt2img-sdxl"), localcfg.SamplingSettings{Steps: &tooMany}},
		"no sampling assignments at all": {templateModel(t, "upscale-nomos-uni-span"), localcfg.SamplingSettings{Steps: &steps}},
		"the steps are a speed mode's":   {templateModel(t, "img2video-wan22-14b-lightx2v"), localcfg.SamplingSettings{Steps: &steps}},
		"no workflow":                    {localcfg.Model{}, localcfg.SamplingSettings{Steps: &steps}},
	} {
		_, err := samplefile.Model(tc.model, tc.s)
		assert.Error(t, err, name)
	}
}

// The author's steps can reach the template's high ones: the "high" button must not stay at or below the workflow's steps (localcfg refuses
// that), and the buttons that still lie on their side stay. The template is not changed.
func TestModelDropsAQualityPresetThatIsNoLongerOnItsSide(t *testing.T) {
	base := templateModel(t, "txt2img-sdxl")
	require.Equal(t, 15, *base.Workflow.QualityPresets.Fast.Steps)
	require.Equal(t, 40, *base.Workflow.QualityPresets.High.Steps)

	for name, tc := range map[string]struct {
		steps      int
		fast, high bool
	}{
		"between the presets": {30, true, true},
		"as high as the high": {40, true, false},
		"above the high":      {45, true, false},
		"as low as the fast":  {15, false, true},
		"below the fast":      {10, false, true},
	} {
		m, err := samplefile.Model(base, localcfg.SamplingSettings{Steps: &tc.steps})
		require.NoError(t, err, name)
		p := m.Workflow.QualityPresets
		assert.Equal(t, tc.fast, p != nil && p.Fast != nil, name+": fast")
		assert.Equal(t, tc.high, p != nil && p.High != nil, name+": high")
		assert.Empty(t, localcfg.ValidateSaved(m), name)
	}
	assert.Equal(t, 40, *base.Workflow.QualityPresets.High.Steps, "the template is not changed")

	m, err := samplefile.Model(base, localcfg.SamplingSettings{Sampler: "euler_ancestral"})
	require.NoError(t, err)
	assert.Equal(t, base.Workflow.QualityPresets, m.Workflow.QualityPresets, "steps not named, presets untouched")
}

// A preset whose steps the author's steps passed keeps the cfg it has (the buttons still differ in cfg), and one left with nothing is dropped; the
// workflow left with no preset has no quality to choose. A preset set made for the test, so that the template's cfg values do not matter.
func TestModelKeepsTheCFGOfAPresetWhoseStepsAreDropped(t *testing.T) {
	steps, cfg := 40, 7.0
	base := templateModel(t, "txt2img-sdxl")
	withPresets := func(p *sampling.Presets) localcfg.Model {
		wf := *base.Workflow
		wf.QualityPresets = p
		m := base
		m.Workflow = &wf
		return m
	}

	m, err := samplefile.Model(withPresets(&sampling.Presets{High: &sampling.Preset{Steps: &steps, CFG: &cfg}}), localcfg.SamplingSettings{Steps: &steps})
	require.NoError(t, err)
	require.NotNil(t, m.Workflow.QualityPresets)
	assert.Equal(t, &sampling.Preset{CFG: &cfg}, m.Workflow.QualityPresets.High, "the steps are dropped, the cfg stays")
	assert.Nil(t, m.Workflow.QualityPresets.Fast)

	m, err = samplefile.Model(withPresets(&sampling.Presets{High: &sampling.Preset{Steps: &steps}}), localcfg.SamplingSettings{Steps: &steps})
	require.NoError(t, err)
	assert.Nil(t, m.Workflow.QualityPresets, "a preset with nothing left is dropped, and so are the presets with it")
}
