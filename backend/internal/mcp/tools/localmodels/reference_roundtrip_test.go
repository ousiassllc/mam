package localmodels_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/localmodels"
)

// stored is the workflow saved for the model (the file, as the generation reads it).
func (r *rig) stored(t *testing.T, name string) localcfg.Workflow {
	t.Helper()
	v, err := r.svc.View()
	require.NoError(t, err)
	for _, m := range v.Models {
		if m.Name == name {
			require.NotNil(t, m.Workflow)
			return *m.Workflow
		}
	}
	require.Failf(t, "model not found", name)
	return localcfg.Workflow{}
}

// sameWorkflow compares two workflows as JSON: the file compacts and reindents the raw values (graph, overrides).
func sameWorkflow(t *testing.T, want, got localcfg.Workflow, msg string) {
	t.Helper()
	w, err := json.Marshal(want)
	require.NoError(t, err)
	g, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, string(w), string(g), msg)
}

func templateWorkflow(t *testing.T, id string) localcfg.Workflow {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok)
	return *tpl.Model.Workflow
}

// An unrelated change (a price) leaves everything the reference images of a bundled model rest on as saved: the mode, the
// optional slots (all ten of them), the LoRA node, the denoise floor and what a run without a reference changes.
func TestUpdateOfAPriceKeepsTheReferenceFieldsOfABundledModel(t *testing.T) {
	r := newRig(t, false, "")
	env := newEnv(t, r)
	env.CallOK("create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": "http://127.0.0.1:8188"}, nil)
	for _, id := range []string{"txt2img-qwen-image21", "txt2img-chroma", "txt2img-flux-dev-lora"} {
		env.CallOK("create_local_model", map[string]any{"endpoint": "box", "name": id, "template_id": id}, nil)
		var up localmodels.Model
		env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": id, "cost_per_image_usd": 0.3}, &up)
		assert.Equal(t, 0.3, up.CostPerImageUSD)
		sameWorkflow(t, templateWorkflow(t, id), r.stored(t, id), id)
	}
}

// The assignments of get_local_model sent back as they are, and sent back without "optional" (a client that does not know it),
// stay optional; "optional": false says it is not.
func TestAssignmentsSentBackKeepTheirOptionalFlag(t *testing.T) {
	r := newRig(t, false, "")
	env := newEnv(t, r)
	env.CallOK("create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": "http://127.0.0.1:8188"}, nil)
	env.CallOK("create_local_model", map[string]any{"endpoint": "box", "name": "qwen", "template_id": "txt2img-qwen-image21"}, nil)
	env.CallOK("create_local_model", map[string]any{"endpoint": "box", "name": "chroma", "template_id": "txt2img-chroma"}, nil)

	var got localmodels.Model
	env.CallOK("get_local_model", map[string]any{"endpoint": "box", "name": "qwen"}, &got)
	require.Len(t, got.Assignments, 15)
	require.NotNil(t, got.Assignments[5].Optional, "the optional flag is shown")
	assert.True(t, *got.Assignments[5].Optional)

	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "qwen", "assignments": got.Assignments}, nil)
	sameWorkflow(t, templateWorkflow(t, "txt2img-qwen-image21"), r.stored(t, "qwen"), "")

	var bare []map[string]any // as an older client sends them
	for _, a := range got.Assignments {
		bare = append(bare, map[string]any{"param": a.Param, "node": a.Node, "input": a.Input, "index": a.Index})
	}
	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "qwen", "assignments": bare}, nil)
	sameWorkflow(t, templateWorkflow(t, "txt2img-qwen-image21"), r.stored(t, "qwen"), "optional is taken from the saved assignment")

	bare[5]["optional"] = false
	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "qwen", "assignments": bare}, nil)
	w := r.stored(t, "qwen")
	assert.False(t, w.Assignments[5].Optional)
	assert.True(t, w.Assignments[6].Optional)
	assert.Equal(t, localcfg.ModeEdit, w.ReferenceMode)
	assert.Equal(t, "37", w.LoraModelNode)

	// Chroma's table has two width and two height assignments (the result's and the reference scale's): all come back.
	var chroma localmodels.Model
	env.CallOK("get_local_model", map[string]any{"endpoint": "box", "name": "chroma"}, &chroma)
	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "chroma", "assignments": chroma.Assignments, "cost_per_image_usd": 0.1}, nil)
	cw := r.stored(t, "chroma")
	sameWorkflow(t, templateWorkflow(t, "txt2img-chroma"), cw, "")
	assert.InDelta(t, 0.6, cw.ReferenceDenoiseMin, 1e-9)
	assert.NotEmpty(t, cw.NoReference)
}
