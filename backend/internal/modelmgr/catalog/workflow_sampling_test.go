package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// What the author of a model recommends is what the model runs with (Issue #994); a model whose author states nothing outside the
// template's own values runs with the template's.
func TestACatalogModelRunsWithTheSamplingItsAuthorRecommends(t *testing.T) {
	type want struct {
		steps, cfg         float64
		sampler, scheduler string
	}
	std := want{25, 7, "dpmpp_2m", "karras"}
	c := catalog.Default()
	for id, w := range map[string]want{
		"noobai-xl-v11":            {25, 5.5, "euler_ancestral", "karras"},
		"wai-illustrious-sdxl-v17": {25, 7, "euler_ancestral", "karras"},
		"realvisxl-v50":            {30, 7, "dpmpp_sde", "karras"},
		"cyberrealistic-pony-v18":  {30, 5, "dpmpp_2m", "karras"},
		"cyberrealistic-xl-v10":    std, // the author's ranges contain the template's values
		"sdxl-base":                std,
	} {
		m, ok := c.Model(id)
		require.True(t, ok, id)
		for _, wf := range m.Workflows {
			model, err := wf.Model(localcfg.TemplateByID)
			require.NoError(t, err, wf.Name)
			caps := model.Workflow.SamplingCaps()
			require.NotNil(t, caps, wf.Name)
			assert.Equal(t, w.steps, caps.Steps.Default, wf.Name)
			assert.Equal(t, w.cfg, caps.CFG.Default, wf.Name)
			assert.Equal(t, w.sampler, caps.Sampler.Default, wf.Name)
			assert.Equal(t, w.scheduler, caps.Scheduler.Default, wf.Name)
		}
	}
}

// A sampling the template cannot take is a mistake of the catalog, found by the validation.
func TestACatalogSamplingTheTemplateCannotTakeFailsTheValidation(t *testing.T) {
	steps := 500
	w := catalog.Workflow{Name: "x", Template: "txt2img-sdxl", Sampling: &localcfg.SamplingSettings{Steps: &steps}}
	_, err := w.Model(localcfg.TemplateByID)
	assert.Error(t, err)
}
