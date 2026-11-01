package localmodels_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/localmodels"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/settings"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotgen"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shottest"
)

// A model defined and assigned with the tools generates through the existing generation tools: generate_shot
// answers with a job_id, get_job reports the job, and the Take that comes out is made by the ComfyUI endpoint
// and carries the identifier of the model.
func TestGenerationWithALocalModelRunsThroughTheExistingJobTools(t *testing.T) {
	comfy := comfyuitest.New(t)
	comfy.OutputNode = "58"
	comfy.OutputFile = "out.mp4"
	r := newRig(t, false, "")

	f := shottest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) {
		env.Services.LocalModels = r.svc
		reg.Settings = mcpkit.Settings{Models: r.models}
		jobs.Register(reg, env.Services)
		shotgen.Register(reg, env.Services)
		localmodels.Register(reg, env.Services)
		settings.Register(reg, env.Services)
	}, shottest.Options{Models: r.adapter})

	f.CallOK("create_local_endpoint", map[string]any{"name": "gpu", "kind": "comfyui", "base_url": comfy.URL()}, nil)
	f.CallOK("create_local_model", map[string]any{"endpoint": "gpu", "name": "wan", "template_id": "txt2video-wan22-5b", "cost_per_second_usd": 0.01}, nil)
	var section settings.ModelsSection
	target := map[string]any{"model": "comfyui:gpu/wan", "resolution": "480p"}
	f.CallOK("set_model_assignment", map[string]any{"policies": []map[string]any{
		{"quality_policy": "fast", "primary": target, "fallback": target, "takes": 1},
		{"quality_policy": "balanced", "primary": target, "fallback": target, "takes": 1},
		{"quality_policy": "max_quality", "primary": target, "fallback": target, "takes": 1},
	}}, &section)

	var out shotgen.ShotJobOutput
	f.CallOK("generate_shot", map[string]any{"shot_id": f.ShotID}, &out)
	require.NotEmpty(t, out.JobID)

	st := f.WaitJob(out.JobID, 30*time.Second, 10*time.Millisecond, "the job never finished")
	assert.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	f.WaitIdle(t)

	assert.GreaterOrEqual(t, comfy.Count("POST /prompt"), 1, "the workflow ran on the endpoint")
	assert.Equal(t, 1, f.Count(t, `SELECT COUNT(*) FROM take WHERE model_name = ?`, "comfyui:gpu/wan"), "the Take names the endpoint and the model")
	assert.Equal(t, 1, f.Count(t, `SELECT COUNT(*) FROM cost_ledger_entry WHERE provider = ?`, "comfyui:gpu/wan"), "the Cost Ledger names it too")
}
