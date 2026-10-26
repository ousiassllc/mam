package modelreg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

func TestWorkflowIDRoundTripsAndRefusesAnythingElse(t *testing.T) {
	id := modelreg.WorkflowID("0199d7a0-1111-7222-8333-444455556666", 12)
	assert.Equal(t, "comfy:0199d7a0-1111-7222-8333-444455556666@v12", id)
	wf, v, ok := modelreg.ParseWorkflowID(id)
	assert.True(t, ok)
	assert.Equal(t, "0199d7a0-1111-7222-8333-444455556666", wf)
	assert.Equal(t, 12, v)

	for _, bad := range []string{
		"", "kling-3.0", "comfyui:gpu/wan", "comfy:", "comfy:@v1", "comfy:abc@v1", "comfy:0199d7a0-1111-7222-8333-444455556666",
		"comfy:0199d7a0-1111-7222-8333-444455556666@v0", "comfy:0199d7a0-1111-7222-8333-444455556666@v01",
		"comfy:0199d7a0-1111-7222-8333-444455556666@vx", "comfy:0199d7a0-1111-7222-8333-444455556666@v-1", "comfy:../../x@v1",
	} {
		_, _, ok := modelreg.ParseWorkflowID(bad)
		assert.False(t, ok, bad)
	}
}

func TestLabelIsTheIdentifierUnlessTheWorkflowIsPublished(t *testing.T) {
	t.Cleanup(func() { modelreg.SetLabels(nil) })
	assert.Equal(t, "kling-3.0", modelreg.Label("kling-3.0"))
	modelreg.SetLabels(map[string]string{"comfy:a@v1": "Clip (v1)"})
	assert.Equal(t, "Clip (v1)", modelreg.Label("comfy:a@v1"))
	assert.Equal(t, "comfy:a@v2", modelreg.Label("comfy:a@v2"))
}
