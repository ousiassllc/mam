package mmview_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

func modelWith(name string, workflows ...string) catalog.Model {
	m := catalog.Model{Name: name}
	for _, w := range workflows {
		m.Workflows = append(m.Workflows, catalog.Workflow{Name: w})
	}
	return m
}

// #1023: a model with one workflow is called by the name of its catalog entry, never by the workflow's identifier.
func TestDisplayName_OneWorkflowIsTheNameOfTheEntry(t *testing.T) {
	m := modelWith("Noct Q V4（Qwen-Image 2.1 非検閲・int8）", "noctq-v4-qwen-image21")
	assert.Equal(t, "Noct Q V4（Qwen-Image 2.1 非検閲・int8）", mmview.DisplayName(m, "noctq-v4-qwen-image21"))
}

// A model with several workflows gives each its own card: the one that is the model's own name keeps the name, and the others are told apart by what
// their workflow's name adds ("sdxl-base-img2img" → "（img2img）").
func TestDisplayName_SeveralWorkflowsAreToldApart(t *testing.T) {
	m := modelWith("SDXL base", "sdxl-base", "sdxl-base-img2img", "sdxl-base-inpaint")
	assert.Equal(t, "SDXL base", mmview.DisplayName(m, "sdxl-base"))
	assert.Equal(t, "SDXL base（img2img）", mmview.DisplayName(m, "sdxl-base-img2img"))
	assert.Equal(t, "SDXL base（inpaint）", mmview.DisplayName(m, "sdxl-base-inpaint"))
}

// When no workflow is the bare prefix (LTX-2.5 has t2v / i2v / flf2v), the common words are dropped from each and the rest says which it is.
func TestDisplayName_WorkflowsWithoutABareNameKeepTheirKind(t *testing.T) {
	m := modelWith("LTX-2.5", "ltx-2-5-t2v", "ltx-2-5-i2v", "ltx-2-5-flf2v-enhanced")
	assert.Equal(t, "LTX-2.5（t2v）", mmview.DisplayName(m, "ltx-2-5-t2v"))
	assert.Equal(t, "LTX-2.5（flf2v-enhanced）", mmview.DisplayName(m, "ltx-2-5-flf2v-enhanced"))
}
