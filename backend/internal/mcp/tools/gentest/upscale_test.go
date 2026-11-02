package gentest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// a picture the tools can enlarge: the first result of a batch of flux-2.
func (r *rig) anImage() string {
	r.T.Helper()
	out, _ := r.run(batch("flux-2"))
	r.job(out.JobID)
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	return b.Results[0].ID
}

func TestUpscaleToolEnlargesAResultIntoAnotherOneThatRemembersTheSource(t *testing.T) {
	r := newRig(t, gentesttest.WithUpscaleModels())
	src := r.anImage()

	var list mcpgentest.UpscalersOutput
	r.CallOK("list_generation_upscalers", map[string]any{}, &list)
	require.Len(t, list.Upscalers, 2)
	assert.Equal(t, []int{2, 4}, list.Scales)
	for _, u := range list.Upscalers {
		assert.NotEmpty(t, u.License.Commercial, "the license answer is there: yes, conditional, no or unknown")
		assert.True(t, u.Available)
	}

	var out mcpgentest.UpscaleOutput
	r.CallOK("upscale_generation_result", map[string]any{"result_id": src, "scale": 4, "model": string(gentesttest.UpscaleModel)}, &out)
	r.job(out.JobID)
	require.NotEmpty(t, out.ResultID)
	assert.NotEqual(t, src, out.ResultID)

	var got record.ResultDetail
	r.CallOK("get_generation_result", map[string]any{"result_id": out.ResultID}, &got)
	assert.Equal(t, "succeeded", got.Status)
	assert.Equal(t, string(gentesttest.UpscaleModel), got.Model)
	require.NotNil(t, got.Upscale)
	assert.Equal(t, src, got.Upscale.SourceResultID)
	assert.Equal(t, 4, got.Upscale.Scale)
	assert.Equal(t, "model", got.Upscale.Mode)
}

func TestUpscaleToolRefusesWhatCannotBeEnlarged(t *testing.T) {
	r := newRig(t, gentesttest.WithUpscaleModels())
	src := r.anImage()
	assert.Contains(t, r.CallError("upscale_generation_result", map[string]any{"result_id": src, "scale": 3}), "validation")
	assert.Contains(t, r.CallError("upscale_generation_result", map[string]any{"result_id": src, "scale": 2, "model": "flux-2"}), "validation")
	assert.Contains(t, r.CallError("upscale_generation_result", map[string]any{"result_id": "nope", "scale": 2}), "not_found")

	bare := newRig(t)
	other := bare.anImage()
	assert.Contains(t, bare.CallError("upscale_generation_result", map[string]any{"result_id": other, "scale": 2}), "validation", "no upscale model is installed")
	var list mcpgentest.UpscalersOutput
	bare.CallOK("list_generation_upscalers", map[string]any{}, &list)
	assert.Empty(t, list.Upscalers)
	assert.NotNil(t, list.Upscalers, "an empty list, not null")
}
