package samplefile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
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
