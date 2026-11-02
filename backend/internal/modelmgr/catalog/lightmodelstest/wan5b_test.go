// Package lightmodelstest checks the catalog entries of the light models of Issue #929 (Wan 2.2 TI2V 5B) and #931 (FLUX.2 klein 4B):
// their pins, licenses, workflows and cards. It is a package of its own because the directory of the catalog is at its line budget.
package lightmodelstest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Wan 2.2 TI2V 5B (Issue #929) is the light video model: one set of files, two workflows (the text-to-video one and the one that
// starts from a first frame) that the generation test lists as two models, a VRAM figure that fits a 16 GB card, and a card
// that tells it apart from the 14B (the quality model) so that a user picks 5B when speed matters.
func TestWan5BIsTheLightVideoModelWithBothWorkflows(t *testing.T) {
	c := catalog.Default()
	m, ok := c.Model("wan22-ti2v-5b")
	require.True(t, ok)
	assert.Equal(t, catalog.TierStandard, m.Tier)
	assert.False(t, m.ConsentRequired)
	assert.False(t, m.RequiresHFToken)
	assert.Equal(t, modellicense.Yes, m.License.Commercial, "Apache-2.0 (the model card)")
	assert.Equal(t, "apache-2.0", m.License.ID)

	assert.Equal(t, 16, m.VRAM.MinGB, "fits the 16 GB of the target PC with ComfyUI's native offload")
	assert.True(t, m.VRAM.Estimate, "not measured on a 16 GB card by mam")
	assert.Contains(t, m.VRAM.Source, "offload")

	byName := map[string]string{}
	for _, wf := range m.Workflows {
		byName[wf.Name] = wf.Template
	}
	assert.Equal(t, map[string]string{"wan22-ti2v-5b": "txt2video-wan22-5b", "wan22-ti2v-5b-i2v": "img2video-wan22-5b"}, byName)
	for name, id := range byName {
		tpl, ok := localcfg.TemplateByID(id)
		require.True(t, ok, name)
		caps, ok := tpl.Model.VideoCaps()
		require.True(t, ok, name)
		assert.Equal(t, name == "wan22-ti2v-5b-i2v", caps.FirstFrame, "only the i2v workflow takes a first frame: "+name)
		assert.Equal(t, 24, tpl.Model.Workflow.FPS, name)
	}

	// The two Wan cards say which to pick: 5B for speed and weight, 14B for quality.
	assert.Contains(t, m.Description, "速さ")
	assert.Contains(t, m.Description, "wan22-i2v-14b")
	base, ok := c.Model("wan22-i2v-14b")
	require.True(t, ok)
	assert.Contains(t, base.Description, "wan22-ti2v-5b")
	assert.Less(t, m.TotalSize(), base.TotalSize(), "the 5B download is the smaller one")
}
