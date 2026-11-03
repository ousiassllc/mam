package voicetraining_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/voicereference"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/voicetraining"
)

func TestTraining_AJobGoesFromRunningToSucceededAndTheVoiceIsTheCharacters(t *testing.T) {
	f := newFixture(t, "ok")
	f.reference()
	f.clips(6, 10)

	out := f.start(map[string]any{"epochs_sovits": 2, "epochs_gpt": 3})
	assert.Equal(t, "Mio", out.Character)
	st := f.env.WaitJob(out.JobID, 30*time.Second, 20*time.Millisecond)

	assert.Equal(t, mcpkit.JobSucceeded, st.State)
	assert.Equal(t, voicetraining.KindTrain, st.Kind)
	assert.Equal(t, 100, st.Done)
	require.NotNil(t, st.Result["trained"])
	v := f.view()
	require.NotNil(t, v.Trained)
	assert.Equal(t, 2, v.Trained.SoVITSEpochs)
	assert.Equal(t, 3, v.Trained.GPTEpochs)
	assert.Equal(t, 6, v.Trained.Clips)
	require.NotNil(t, v.Current)
	assert.Equal(t, "succeeded", v.Current.State)

	var after voicetraining.View
	f.env.CallOK("delete_character_trained_voice", f.target(), &after)
	assert.Nil(t, after.Trained, "back to the zero-shot clone")
	f.env.CallOK("delete_character_trained_voice", f.target(), &after)
	var ref voicereference.Output
	f.env.CallOK("update_character_voice_reference", f.target(), &ref)
	assert.NotNil(t, ref.VoiceReference, "the recording stayed")
}

func TestTraining_AFailureIsToldAsTheCommonErrorAndLeavesNoVoice(t *testing.T) {
	f := newFixture(t, "fail")
	f.reference()
	f.clips(6, 10)

	out := f.start(map[string]any{"epochs_sovits": 1, "epochs_gpt": 1})
	st := f.env.WaitJob(out.JobID, 30*time.Second, 20*time.Millisecond)

	assert.Equal(t, mcpkit.JobFailed, st.State)
	require.NotNil(t, st.Failure)
	assert.Equal(t, apperr.CodeTrainFailed, st.Failure.Code)
	assert.Contains(t, st.Failure.Cause, "GPU のメモリが足りなくなりました", "the cause of CUDA running out of memory, in words")
	assert.NotEmpty(t, st.Failure.Remedy)
	assert.Nil(t, f.view().Trained)
}

func TestTraining_ACancelStopsTheJobAndKeepsNothing(t *testing.T) {
	f := newFixture(t, "hang")
	f.reference()
	f.clips(6, 10)
	out := f.start(nil)
	require.Eventually(t, func() bool { m, _ := filepath.Glob(filepath.Join(f.rt.work, "*", "hung")); return len(m) > 0 }, 30*time.Second, 10*time.Millisecond)
	var running mcpkit.JobStatus
	require.Eventually(t, func() bool {
		f.env.CallOK("get_job", map[string]any{"job_id": out.JobID}, &running)
		return running.Steps != nil
	}, 30*time.Second, 20*time.Millisecond)
	assert.Equal(t, mcpkit.JobRunning, running.State)
	require.NotNil(t, running.Steps, "the epoch and the time are the progress of get_job")
	assert.Contains(t, running.Message, "エポック")

	var cancelled voicetraining.CancelOutput
	f.env.CallOK("cancel_character_voice_training", f.target(), &cancelled)

	assert.True(t, cancelled.Cancelled)
	st := f.env.WaitJob(out.JobID, 30*time.Second, 20*time.Millisecond)
	assert.Equal(t, mcpkit.JobCancelled, st.State)
	assert.Nil(t, f.view().Trained)
	assert.Contains(t, f.env.CallError("cancel_character_voice_training", f.target()), "conflict", "nothing runs now")
	entries, _ := os.ReadDir(filepath.Join(f.env.AssetsDir, "voicetrain"))
	for _, e := range entries {
		assert.Equal(t, "_draft", e.Name(), "no weights are left")
	}
}

func TestTraining_ASecondTrainingIsRefusedWhileOneRuns(t *testing.T) {
	f := newFixture(t, "hang")
	f.reference()
	f.clips(6, 10)
	f.start(nil)

	assert.Contains(t, f.env.CallError("start_character_voice_training", f.target()), "conflict")
	assert.True(t, f.view().GPUBusy)
	var c voicetraining.CancelOutput
	f.env.CallOK("cancel_character_voice_training", f.target(), &c)
}
