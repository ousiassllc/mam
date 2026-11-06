// Package gptimagetest runs the GPT Image 2.6 settings (complexity, GIF / SVG output, markup; Issue #1096) through the MCP tools.
package gptimagetest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

type rig struct {
	*mcptest.Env
	GT *gentesttest.Rig
}

func newRig(t *testing.T, opts ...gentesttest.Option) *rig {
	t.Helper()
	r := &rig{}
	r.Env = mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		r.GT = gentesttest.NewOn(t, env.DB, opts...)
		env.Services.Gentest = r.GT.Svc
		jobs.Register(reg, env.Services)
		mcpgentest.Register(reg, env.Services)
	})
	return r
}

func (r *rig) job(id string) mcpkit.JobStatus {
	r.T.Helper()
	return r.WaitJob(id, 20*time.Second, 10*time.Millisecond)
}

// run estimates the batch and runs it with the figures it was shown.
func (r *rig) run(args map[string]any) (mcpgentest.RunOutput, gentest.Estimate) {
	r.T.Helper()
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", run, &out)
	return out, est
}

func (r *rig) stagePNG(data []byte) string {
	r.T.Helper()
	id, _, err := r.GT.Svc.StageReference(data)
	require.NoError(r.T, err)
	return id
}

// #1096: the list says what GPT Image 2.6 takes, and the run's gpt_image and markup reach the call; what a model cannot take is a validation error (never dropped).
func TestGPTImageSettingsAndMarkupFromMCP(t *testing.T) {
	r := newRig(t, gentesttest.WithSubscriptionImageModels())
	var listed mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "image"}, &listed)
	by := map[string]modelcatalog.Info{}
	for _, m := range listed.Models {
		by[m.ID] = m
	}
	flare := by[string(domain.ModelGPTImage26Flare)]
	require.NotNil(t, flare.GPTImage)
	assert.Contains(t, flare.ReferenceRoles, "markup")
	assert.Empty(t, flare.GPTImageRefusal)
	assert.Equal(t, "unknown", flare.Price.Basis)
	assert.Nil(t, by[string(domain.ModelGPTImage2)].GPTImage)
	sub := by[string(domain.ModelGPTImage26Flare)+":chatgpt-subscription"]
	assert.Nil(t, sub.GPTImage)
	assert.Contains(t, sub.GPTImageRefusal, "サブスク")

	base := r.stagePNG(gentesttest.SolidPNG(4, 3, 255))
	markup := r.stagePNG(gentesttest.SolidPNG(4, 3, 0))
	args := map[string]any{
		"media": "image", "models": []string{string(domain.ModelGPTImage26Flare)}, "prompts": []map[string]any{{"prompt": "circle the cat"}},
		"references": []map[string]any{{"source": "upload", "id": base}, {"source": "upload", "id": markup, "role": "markup"}},
		"gpt_image":  map[string]any{"complexity": "low", "output_format": "gif", "frame_count": 5, "frame_duration_ms": 90},
	}
	out, _ := r.run(args)
	require.Equal(t, "succeeded", string(r.job(out.JobID).State))
	call := r.GT.Fake.CallsOf(domain.ModelGPTImage26Flare)[0].Image
	assert.Equal(t, gptimage.Options{Complexity: "low", Format: "gif", FrameCount: 5, FrameDurationMS: 90}, call.GPT)
	require.NotNil(t, call.Markup)
	assert.Len(t, call.Inputs, 1)

	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	var res mcpgentest.ResultOutput
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &res)
	require.NotNil(t, res.GPT)
	assert.Equal(t, 5, res.GPT.FrameCount)

	for name, mut := range map[string]func(map[string]any){
		"gpt-image-2": func(a map[string]any) { a["models"] = []string{string(domain.ModelGPTImage2)} },
		"subscription": func(a map[string]any) {
			a["models"] = []string{string(domain.ModelGPTImage26Flare) + ":chatgpt-subscription"}
		},
		"bad frames": func(a map[string]any) { a["gpt_image"] = map[string]any{"output_format": "gif", "frame_count": 40} },
		"svg edit":   func(a map[string]any) { a["gpt_image"] = map[string]any{"output_format": "svg"} },
	} {
		bad := map[string]any{}
		for k, v := range args {
			bad[k] = v
		}
		mut(bad)
		assert.Contains(t, r.CallError("estimate_generation_test", bad), "validation", name)
	}
}
