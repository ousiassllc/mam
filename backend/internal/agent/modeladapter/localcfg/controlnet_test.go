package localcfg_test

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
	coreNodes = append(coreNodes, "ControlNetLoader", "ControlNetApplyAdvanced", "CLIPVisionLoader") // ControlNet and IP-Adapter (Issue #976)
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
// the picture as the control image / the style image, with the whole picture as its only role.
func TestControlNetAndIPAdapterTemplatesTakeOneRequiredReference(t *testing.T) {
	for id, want := range map[string]struct {
		mode  localcfg.ReferenceMode
		nodes []string
	}{
		"txt2img-sdxl-controlnet": {localcfg.ModeControlNet, nil},
		"txt2img-sdxl-ipadapter":  {localcfg.ModeIPAdapter, []string{"ipadapter-plus"}},
	} {
		m := modelOf(t, id)
		caps := m.ReferenceCaps()
		assert.Equal(t, want.mode, caps.Mode, id)
		assert.Equal(t, 1, caps.Max, id)
		assert.Equal(t, []string{localcfg.RoleWhole}, caps.Roles, id)
		assert.Equal(t, want.nodes, m.Workflow.RequiresNodes, id)
		assert.True(t, m.Workflow.Has(localcfg.ParamReferenceStrength), id+": the strength slider of the screen reaches the node")
	}
}

func TestAWorkflowThatCannotRunWithoutItsReferenceIsRefusedWhenItSaysItCan(t *testing.T) {
	for _, id := range []string{"txt2img-sdxl-controlnet", "txt2img-sdxl-ipadapter"} {
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
