package workflows_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/workflows"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// A saved speech-to-video workflow runs with the audio_id of an upload (purpose gentest_audio): the sound goes to the engine and the staged file is consumed once the run
// started; a refused call keeps it so that the call can be made again (Issue #1153).
func TestRunWorkflowTakesTheAudioIDOfAnUploadForASpeechToVideoWorkflow(t *testing.T) {
	r := newRig(t, true)
	r.comfy.OutputFile, r.comfy.OutputNode, r.comfy.OutputBody = "clip.mp4", "113", []byte("mp4")
	w := r.importWorkflow(t, "Talk", "video", "audio2video-wan22-s2v-14b")
	sound := audiofiletest.WAV(16000, 16000)
	id, _, err := r.gt.Svc.StageAudio(sound)
	require.NoError(t, err)
	picture := map[string]any{"project_id": "p", "asset_id": "a"}

	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": w.ID, "prompt": "x", "references": []any{picture}}), "音声ファイルが要ります", "a sound is needed")
	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": w.ID, "prompt": "x", "references": []any{picture}, "audio_id": "0000.wav"}), "validation", "an upload that is gone")
	still := r.importWorkflow(t, "Stills", "image", "txt2img-sdxl")
	assert.Contains(t, r.CallError("run_workflow", map[string]any{"workflow_id": still.ID, "prompt": "x", "audio_id": id}), "音声ファイルを使いません", "a workflow without an audio assignment is not given one")
	_, _, err = r.gt.Svc.StagedFile(id)
	require.NoError(t, err, "the refusals kept the staged sound")

	var out workflows.RunOutput
	r.CallOK("run_workflow", map[string]any{"workflow_id": w.ID, "prompt": "she greets us", "references": []any{picture}, "audio_id": id}, &out)
	st := r.waitJob(t, out.JobID)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Contains(t, r.comfy.UploadBodies(), sound, "the engine got the sound")
	_, _, err = r.gt.Svc.StagedFile(id)
	assert.ErrorIs(t, err, gterr.ErrNotFound, "the staged sound was consumed by the run that started")
}
