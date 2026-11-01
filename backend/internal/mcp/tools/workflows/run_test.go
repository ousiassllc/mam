package workflows_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/director"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/workflows"
)

func (r *rig) waitJob(t *testing.T, id string) mcpkit.JobStatus {
	t.Helper()
	return r.WaitJob(id, 15*time.Second, 10*time.Millisecond, "the job never finished")
}

func TestRunWorkflowReturnsAJobAndATokenURLForTheResult(t *testing.T) {
	r := newRig(t, true)
	w := r.importWorkflow(t, "Stills", "image", "txt2img-sdxl")

	var out workflows.RunOutput
	r.CallOK("run_workflow", map[string]any{"workflow_id": w.ID, "prompt": "a red bicycle", "aspect_ratio": "1:1"}, &out)
	require.NotEmpty(t, out.JobID)
	assert.Equal(t, w.ModelID, out.ModelID)
	assert.NotEmpty(t, out.Warnings, "the fake ComfyUI lacks the nodes: the run starts anyway, with the warnings")

	st := r.waitJob(t, out.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "run_workflow", st.Kind)
	assert.Equal(t, w.ModelID, st.Result["model_id"])
	url, _ := st.Result["download_url"].(string)
	assert.Contains(t, url, "/mcp-files/", "the result is handed over as a URL with a ticket, never as bytes")
	assert.Equal(t, "image/png", st.Result["mime_type"])

	// A failure of the ComfyUI is the job's failure with its reason.
	r.comfy.FailWith = "out of memory"
	r.CallOK("run_workflow", map[string]any{"workflow_id": w.ID, "prompt": "x"}, &out)
	st = r.waitJob(t, out.JobID)
	assert.Equal(t, mcpkit.JobFailed, st.State)
	assert.Contains(t, st.Error, "out of memory")

	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": w.ID}), "prompt", "a prompt is needed")
	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": w.ID, "prompt": "x", "first_frame": map[string]any{"project_id": "p", "asset_id": "a"}}), "validation", "an image workflow has no first frame")
	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": "missing", "prompt": "x"}), "not_found")
	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": w.ID, "version": 7, "prompt": "x"}), "not_found")
}

func TestSetShotWorkflowPinsTheVideoModelLockAndExistingToolsAcceptTheID(t *testing.T) {
	r := newRig(t, true)
	clip := r.importWorkflow(t, "Clip", "video", "img2video-wan22-5b")
	still := r.importWorkflow(t, "Stills", "image", "txt2img-sdxl")

	var pinned workflows.SetShotWorkflowOutput
	r.CallOK("set_shot_workflow", map[string]any{"shot_id": r.ShotID, "workflow_id": clip.ID}, &pinned)
	assert.Equal(t, clip.ModelID, pinned.ModelID)
	locks := func() map[string]director.Lock {
		var out director.InspectorOutput
		r.CallOK("get_shot_inspector", map[string]any{"shot_id": r.ShotID}, &out)
		return out.Locks
	}
	assert.Equal(t, director.Lock{Mode: "lock", Value: clip.ModelID}, locks()["video_model"])
	r.CallOK("set_shot_workflow", map[string]any{"shot_id": r.ShotID, "workflow_id": clip.ID}, &pinned) // the same pin again

	// The version can be edited away; the pin still names the one it pinned, and it still runs.
	var detail workflows.GetOutput
	r.CallOK("get_workflow", map[string]any{"workflow_id": clip.ID}, &detail)
	settings := detail.Settings
	r.CallOK("update_workflow_assignment", map[string]any{"workflow_id": clip.ID, "assignments": settings.Assignments, "output_node": settings.OutputNode, "size_multiple": 64, "video": settings.Video}, nil)
	assert.Equal(t, clip.ModelID, locks()["video_model"].Value)
	assert.Contains(t, r.CallError("set_shot_workflow", map[string]any{"shot_id": r.ShotID, "workflow_id": clip.ID}), "locked", "the Shot is pinned to version 1: unlock before moving it")

	// The existing lock tools accept the identifier as well (MANUAL on another Shot's worth of validation).
	var out director.LocksOutput
	r.CallOK("set_shot_lock", map[string]any{"shot_id": r.ShotID, "parameter": "video_model", "mode": "auto"}, &out)
	r.CallOK("set_shot_lock", map[string]any{"shot_id": r.ShotID, "parameter": "video_model", "mode": "manual", "value": clip.ModelID}, &out)
	assert.Equal(t, clip.ModelID, out.Locks["video_model"].Value)
	assert.Contains(t, r.CallError("set_shot_lock", map[string]any{"shot_id": r.ShotID, "parameter": "video_model", "mode": "manual", "value": "comfy:no-such-workflow@v1"}), "validation")
	assert.Contains(t, r.CallError("set_shot_lock", map[string]any{"shot_id": r.ShotID, "parameter": "video_model", "mode": "manual", "value": still.ModelID}), "validation", "an image workflow is not a Video Model")

	r.CallOK("set_shot_lock", map[string]any{"shot_id": r.ShotID, "parameter": "video_model", "mode": "auto"}, &out)
	assert.Contains(t, r.CallError("set_shot_workflow", map[string]any{"shot_id": r.ShotID, "workflow_id": still.ID}), "validation", "only a video workflow")
	assert.Contains(t, r.CallError("set_shot_workflow", map[string]any{"shot_id": r.ShotID, "workflow_id": clip.ID, "version": 9}), "not_found")
	assert.Contains(t, r.CallError("set_shot_workflow", map[string]any{"shot_id": "missing", "workflow_id": clip.ID}), "not_found")
}
