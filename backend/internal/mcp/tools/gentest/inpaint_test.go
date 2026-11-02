package gentest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

func TestInpaintFromMCP(t *testing.T) {
	r := newRig(t, gentesttest.WithInpaintModel())
	var listed mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "image"}, &listed)
	var info modelcatalog.Info
	for _, m := range listed.Models {
		if m.ID == string(gentesttest.InpaintModel) {
			info = m
		}
	}
	assert.True(t, info.SupportsInpaint)
	require.NotNil(t, info.DenoiseDefault)
	assert.InDelta(t, 0.75, *info.DenoiseDefault, 1e-9)
	src := r.stageUploads(1)
	id, _, err := r.GT.Svc.StageReference(gentesttest.WhitePNG)
	require.NoError(t, err)
	args := map[string]any{
		"media": "image", "models": []string{string(gentesttest.InpaintModel)},
		"prompts": []map[string]any{{"prompt": "a fox"}}, "references": src,
		"mask": map[string]any{"source": "upload", "id": id}, "denoise": 0.55,
	}
	out, _ := r.run(args)
	require.Equal(t, "succeeded", string(r.job(out.JobID).State))
	call := r.GT.Fake.CallsOf(gentesttest.InpaintModel)[0].Image
	require.NotNil(t, call.Inpaint)
	require.NotNil(t, call.Inpaint.Denoise)
	assert.InDelta(t, 0.55, *call.Inpaint.Denoise, 1e-9)
	assert.NotEmpty(t, call.Inpaint.Mask.Data)
	plain := map[string]any{
		"media": "image", "models": []string{string(gentesttest.PlainModel)},
		"prompts": []map[string]any{{"prompt": "a fox"}}, "references": src,
		"mask": map[string]any{"source": "upload", "id": id},
	}
	res := r.Call("estimate_generation_test", plain)
	require.True(t, res.IsError)
}
