package localmodels_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/tools/localmodels"
)

// The gap of the Mega first-and-last-frame workflow is the frame count minus two (offset -2 of a frames assignment, Issue #1076): an update that sends the
// assignments back, with or without "offset" (a client that does not know it), keeps it; an offset on anything but frames is refused.
func TestAssignmentsSentBackKeepTheirFramesOffset(t *testing.T) {
	const template = "flf2v-wan22-14b-rapid-mega"
	r := newRig(t, false, "")
	env := newEnv(t, r)
	env.CallOK("create_local_endpoint", map[string]any{"name": "box", "kind": "comfyui", "base_url": "http://127.0.0.1:8188"}, nil)
	env.CallOK("create_local_model", map[string]any{"endpoint": "box", "name": "mega", "template_id": template}, nil)

	var got localmodels.Model
	env.CallOK("get_local_model", map[string]any{"endpoint": "box", "name": "mega"}, &got)
	offsets := 0
	for _, a := range got.Assignments {
		if a.Offset != nil {
			offsets++
			assert.Equal(t, "frames", a.Param)
			assert.Equal(t, -2, *a.Offset)
		}
	}
	require.Equal(t, 2, offsets, "the gray frames of the control video and the white frames of the mask")

	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "mega", "assignments": got.Assignments}, nil)
	sameWorkflow(t, templateWorkflow(t, template), r.stored(t, "mega"), "sent back as they are")

	var bare []map[string]any // as a client that does not know offset sends them
	for _, a := range got.Assignments {
		bare = append(bare, map[string]any{"param": a.Param, "node": a.Node, "input": a.Input, "index": a.Index})
	}
	env.CallOK("update_local_model", map[string]any{"endpoint": "box", "name": "mega", "assignments": bare}, nil)
	sameWorkflow(t, templateWorkflow(t, template), r.stored(t, "mega"), "offset is taken from the saved assignment")

	// an offset on the seed is refused
	for i := range bare {
		if bare[i]["param"] == "seed" {
			bare[i]["offset"] = -2
		}
	}
	assert.Contains(t, env.CallError("update_local_model", map[string]any{"endpoint": "box", "name": "mega", "assignments": bare}), "offset")
}
