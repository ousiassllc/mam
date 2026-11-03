package localmodels_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/localmodels"
)

// The hand-over step of Wan 2.2's two samplers is a share of the steps (ratio): an update that sends the assignments back, with or
// without "ratio" (a client that does not know it), keeps it; the prompt weight value and the sampling assignments stay with the model.
func TestAssignmentsSentBackKeepTheirRatio(t *testing.T) {
	r := newRig(t, false, "")
	env := newEnv(t, r)
	env.CallOK("create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": "http://127.0.0.1:8188"}, nil)
	env.CallOK("create_local_model", map[string]any{"endpoint": "box", "name": "wan", "template_id": "img2video-wan22-14b"}, nil)

	var got localmodels.Model
	env.CallOK("get_local_model", map[string]any{"endpoint": "box", "name": "wan"}, &got)
	ratios := 0
	for _, a := range got.Assignments {
		if a.Ratio != nil {
			ratios++
			assert.Equal(t, "steps", a.Param)
			assert.InDelta(t, 0.5, *a.Ratio, 1e-9)
		}
	}
	require.Equal(t, 2, ratios, "end_at_step of the first sampler and start_at_step of the second")

	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "wan", "assignments": got.Assignments}, nil)
	sameWorkflow(t, templateWorkflow(t, "img2video-wan22-14b"), r.stored(t, "wan"), "sent back as they are")

	var bare []map[string]any // as a client that does not know ratio sends them
	for _, a := range got.Assignments {
		bare = append(bare, map[string]any{"param": a.Param, "node": a.Node, "input": a.Input, "index": a.Index})
	}
	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "wan", "assignments": bare}, nil)
	sameWorkflow(t, templateWorkflow(t, "img2video-wan22-14b"), r.stored(t, "wan"), "ratio is taken from the saved assignment")

	// a ratio of 1 hands over nothing: the update is refused
	for i := range bare {
		if bare[i]["input"] == "end_at_step" {
			bare[i]["ratio"] = 1
		}
	}
	assert.Contains(t, env.CallError("update_local_model", map[string]any{"endpoint": "box", "name": "wan", "assignments": bare}), "ratio")
}
