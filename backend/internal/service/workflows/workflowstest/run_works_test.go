package workflowstest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// A run is recorded for the job list (worktrack, memory only) under the id of its record: running while ComfyUI works on it, then
// succeeded or failed with the reason the record carries.
func TestRunIsRecordedForTheJobListFromRunningToItsEnd(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	assert.Empty(t, r.svc.Works(), "nothing ran")

	r.comfy.Hold = make(chan struct{})
	rec, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "a red bicycle"})
	require.NoError(t, err)
	works := r.svc.Works()
	require.Len(t, works, 1)
	assert.Equal(t, rec.ID, works[0].ID)
	assert.Equal(t, worktrack.KindWorkflowRun, works[0].Kind)
	assert.Equal(t, worktrack.Running, works[0].State)
	assert.Equal(t, "Stills", works[0].Subject)
	assert.Equal(t, w.ModelID, works[0].ModelID)
	assert.NotEmpty(t, works[0].Text)

	close(r.comfy.Hold)
	require.Equal(t, workflows.RunSucceeded, wait(t, r, rec.ID).Status)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got := r.svc.Works()
		require.Len(c, got, 1)
		assert.Equal(c, worktrack.Succeeded, got[0].State)
		assert.NotNil(c, got[0].FinishedAt)
	}, 5*time.Second, 5*time.Millisecond)

	r.comfy.Hold = nil
	r.comfy.FailWith = "out of memory"
	failed, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "x"})
	require.NoError(t, err)
	require.Equal(t, workflows.RunFailed, wait(t, r, failed.ID).Status)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		got := r.svc.Works()
		require.Len(c, got, 2)
		assert.Equal(c, failed.ID, got[0].ID, "newest first")
		assert.Equal(c, worktrack.Failed, got[0].State)
		assert.Contains(c, got[0].Error, "out of memory")
	}, 5*time.Second, 5*time.Millisecond)
}
