package configtest

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
)

func init() {
	coreNodes = append(coreNodes, "ControlNetLoader", "ControlNetApplyAdvanced", "CLIPVisionLoader", // ControlNet and IP-Adapter (Issue #976)
		"SetUnionControlNetType") // comfy_extras/nodes_controlnet.py: the type of a Union ControlNet (the normal map, Issue #1020)
}

// assertNodeAllowed: a node of a template is a core node of ComfyUI, or comes from a custom node of the allowlist that the template declares
// (requires_nodes). Nothing else may be in a template.
func assertNodeAllowed(t *testing.T, tpl localcfg.Template, class string) {
	t.Helper()
	custom := nodecat.ClassesOf(nodecat.Default(), tpl.Model.Workflow.RequiresNodes)
	assert.True(t, slices.Contains(coreNodes, class) || slices.Contains(custom, class),
		"%s: %s is neither a core node of ComfyUI %s nor of a node the template requires", tpl.ID, class, coreVersion)
}

func modelOf(t *testing.T, id string) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return tpl.Model
}

// ControlNet (core nodes) and IP-Adapter (a custom node) both take exactly one reference image, which cannot be left out: the screen offers
// the picture as the control image / the style image. The IP-Adapter takes the whole picture only; a ControlNet takes a pose picture too (the
// depth and normal maps of the pose library are given as the pose, Issue #1020).
func TestControlNetAndIPAdapterTemplatesTakeOneRequiredReference(t *testing.T) {
	for id, want := range map[string]struct {
		mode  localcfg.ReferenceMode
		roles []string
		nodes []string
	}{
		"txt2img-sdxl-controlnet":        {localcfg.ModeControlNet, []string{localcfg.RoleWhole, localcfg.RolePose}, nil},
		"txt2img-sdxl-controlnet-depth":  {localcfg.ModeControlNet, []string{localcfg.RoleWhole, localcfg.RolePose}, nil},
		"txt2img-sdxl-controlnet-normal": {localcfg.ModeControlNet, []string{localcfg.RoleWhole, localcfg.RolePose}, nil},
		"txt2img-sdxl-ipadapter":         {localcfg.ModeIPAdapter, []string{localcfg.RoleWhole}, []string{"ipadapter-plus"}},
	} {
		m := modelOf(t, id)
		caps := m.ReferenceCaps()
		assert.Equal(t, want.mode, caps.Mode, id)
		assert.Equal(t, 1, caps.Max, id)
		assert.Equal(t, want.roles, caps.Roles, id)
		assert.Equal(t, want.nodes, m.Workflow.RequiresNodes, id)
		assert.True(t, m.Workflow.Has(localcfg.ParamReferenceStrength), id+": the strength slider of the screen reaches the node")
	}
}

// The depth and normal templates (Issue #1020) are the canny one with another ControlNet file, and no preprocessor: the control image is
// the map that the user (or the pose library) gives, which is a depth or a normal map already, so mam does not make one from a photograph.
// The normal one goes through SetUnionControlNetType (the Union model needs its type told), and the ControlNet the sampler's conditioning
// comes from is the one that node made.
func TestDepthAndNormalControlNetTemplatesTakeTheMapAsItIs(t *testing.T) {
	canny := modelOf(t, "txt2img-sdxl-controlnet")
	assert.Equal(t, "canny", canny.Workflow.ReferencePreprocess, "the canny template still draws its edges")
	for id, file := range map[string]string{
		"txt2img-sdxl-controlnet-depth":  "xinsir-controlnet-depth-sdxl-1.0.safetensors",
		"txt2img-sdxl-controlnet-normal": "xinsir-controlnet-union-sdxl-1.0.safetensors",
	} {
		m := modelOf(t, id)
		assert.Empty(t, m.Workflow.ReferencePreprocess, id+": no preprocessing")
		assert.Equal(t, localcfg.ModeControlNet, m.Workflow.ReferenceMode, id)
		assert.Empty(t, localcfg.ValidateModel("box/x", m, comfy()), id)
		g, err := localcfg.ParseGraph(m.Workflow.Graph)
		require.NoError(t, err)
		var name string
		require.NoError(t, json.Unmarshal(g["12"].Inputs["control_net_name"], &name))
		assert.Equal(t, file, name, id)
		for _, want := range [][2]string{{"13", "image"}, {"13", "positive"}, {"13", "negative"}} {
			assert.Contains(t, g[want[0]].Inputs, want[1], id)
		}
		var ctl []any // the ControlNet the apply node reads
		require.NoError(t, json.Unmarshal(g["13"].Inputs["control_net"], &ctl))
		if id == "txt2img-sdxl-controlnet-depth" {
			assert.Equal(t, []any{"12", float64(0)}, ctl, "the loader feeds the apply node directly")
			assert.NotContains(t, g, "14")
			continue
		}
		assert.Equal(t, []any{"14", float64(0)}, ctl, "the apply node reads the ControlNet whose type was set")
		assert.Equal(t, "SetUnionControlNetType", g["14"].ClassType)
		var typ string
		require.NoError(t, json.Unmarshal(g["14"].Inputs["type"], &typ))
		assert.Equal(t, "normal", typ)
		var from []any
		require.NoError(t, json.Unmarshal(g["14"].Inputs["control_net"], &from))
		assert.Equal(t, []any{"12", float64(0)}, from, "the type is set on the loaded Union ControlNet")
	}
}

func TestAWorkflowThatCannotRunWithoutItsReferenceIsRefusedWhenItSaysItCan(t *testing.T) {
	for _, id := range []string{"txt2img-sdxl-controlnet", "txt2img-sdxl-controlnet-depth", "txt2img-sdxl-controlnet-normal", "txt2img-sdxl-ipadapter"} {
		t.Run("optional slot "+id, func(t *testing.T) {
			m := modelOf(t, id)
			w := *m.Workflow
			w.Assignments = append([]localcfg.Assignment(nil), w.Assignments...)
			for i, a := range w.Assignments {
				if a.Param == localcfg.ParamReferenceImage {
					w.Assignments[i].Optional = true
				}
			}
			m.Workflow = &w
			errs := localcfg.ValidateModel("box/x", m, comfy())
			require.NotEmpty(t, errs)
			assert.ErrorContains(t, errs[0], "cannot run without its reference image")
		})
		t.Run("no_reference "+id, func(t *testing.T) {
			m := modelOf(t, id)
			w := *m.Workflow
			w.NoReference = []localcfg.Override{{Node: "3", Input: "denoise", Value: json.RawMessage("1")}}
			m.Workflow = &w
			assert.NotEmpty(t, localcfg.ValidateModel("box/x", m, comfy()))
		})
		t.Run("two references "+id, func(t *testing.T) {
			m := modelOf(t, id)
			w := *m.Workflow
			w.Assignments = append(append([]localcfg.Assignment(nil), w.Assignments...), localcfg.Assignment{Param: localcfg.ParamReferenceImage, Node: "10", Input: "image", Index: 1})
			m.Workflow = &w
			assert.NotEmpty(t, localcfg.ValidateModel("box/x", m, comfy()))
		})
	}
}

func TestRequiresNodesNamesAreIDs(t *testing.T) {
	m := modelOf(t, "txt2img-sdxl-ipadapter")
	w := *m.Workflow
	w.RequiresNodes = []string{"ipadapter-plus", "../etc"}
	m.Workflow = &w
	errs := localcfg.ValidateModel("box/x", m, comfy())
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "requires_nodes")
}
