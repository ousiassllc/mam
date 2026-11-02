// Package opengraphtest pins the graph "今の設定を ComfyUI で開く" hands to the node screen (comfyui.GraphFor, Issue #977).
package opengraphtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/stage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// required are the inputs a node of the class cannot be queued without (the ones the reference images feed).
var required = map[string][]string{
	"ControlNetApplyAdvanced": {"image"},
	"IPAdapterAdvanced":       {"image"},
	"VAEEncode":               {"pixels"},
	"SetLatentNoiseMask":      {"samples", "mask"},
	"ImageCompositeMasked":    {"destination", "source"},
	"ImageToMask":             {"image"},
}

func graphFor(t *testing.T, tp localcfg.Template) localcfg.Graph {
	t.Helper()
	seed := int64(7)
	raw, err := comfyui.GraphFor(*tp.Model.Workflow, modeladapter.ImageRequest{Prompt: "a fox", AspectRatio: "1:1", Seed: &seed})
	require.NoError(t, err)
	g, err := localcfg.ParseGraph(raw)
	require.NoError(t, err)
	return g
}

// The node screen gets a graph it can run: no link goes to a node that is gone, and no node lacks an input it cannot do without. A workflow that
// cannot run without its input image (ControlNet, IP-Adapter, inpaint) keeps the nodes of that image: the image is the file the workflow holds,
// which the user replaces on the node screen.
func TestTheGraphOfEveryImageTemplateIsWhole(t *testing.T) {
	for _, tp := range localcfg.Templates() {
		if tp.Media != localcfg.MediaImage {
			continue
		}
		t.Run(tp.ID, func(t *testing.T) {
			g := graphFor(t, tp)
			for id, n := range g {
				for name, raw := range n.Inputs {
					if to, _, ok := stage.LinkOf(raw); ok {
						assert.Contains(t, g, to, "%s (%s).%s links to a node that is gone", id, n.ClassType, name)
					}
				}
				for _, name := range required[n.ClassType] {
					assert.Contains(t, n.Inputs, name, "%s (%s) lacks %s", id, n.ClassType, name)
				}
			}
		})
	}
}

func TestAWorkflowThatNeedsItsInputImageKeepsItsNodes(t *testing.T) {
	for _, id := range []string{"txt2img-sdxl-controlnet", "txt2img-sdxl-ipadapter", "inpaint-sdxl", "inpaint-flux-dev"} {
		t.Run(id, func(t *testing.T) {
			tp, ok := localcfg.TemplateByID(id)
			require.True(t, ok)
			g := graphFor(t, tp)
			shipped, err := localcfg.ParseGraph(tp.Model.Workflow.Graph)
			require.NoError(t, err)
			for nodeID, n := range shipped {
				assert.Contains(t, g, nodeID, "the node %s (%s) of the template is gone", nodeID, n.ClassType)
			}
			for _, a := range tp.Model.Workflow.Assignments {
				if a.Param == localcfg.ParamReferenceImage || a.Param == localcfg.ParamMaskImage {
					assert.JSONEq(t, string(shipped[a.Node].Inputs[a.Input]), string(g[a.Node].Inputs[a.Input]), "the file of %s is the workflow's", a.Node)
				}
			}
		})
	}
}
