package workflowstest_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// wait polls the record of a run until it is no longer running.
func wait(t *testing.T, r *rig, id string) workflows.RunRecord {
	t.Helper()
	var rec workflows.RunRecord
	require.Eventually(t, func() bool {
		var err error
		rec, err = r.svc.Run(context.Background(), id)
		require.NoError(t, err)
		return rec.Status != workflows.RunRunning
	}, 10*time.Second, 5*time.Millisecond)
	return rec
}

func TestRunExecutesTheStoredGraphWithTheAssignmentAndKeepsTheResult(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)

	rec, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "a red bicycle", AspectRatio: "1:1"})
	require.NoError(t, err)
	assert.Equal(t, w.ModelID, rec.ModelID)
	assert.NotEmpty(t, rec.Warnings, "the fake server lacks the nodes: the run warns and still runs")
	done := wait(t, r, rec.ID)
	require.Equal(t, workflows.RunSucceeded, done.Status, done.Error)
	assert.Equal(t, "image/png", done.MimeType)

	path, mime, err := r.svc.RunFile(ctx, rec.ID)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, comfyuitest.PNG, data)

	var prompt string
	for _, a := range spec.Assignments {
		if a.Param == localcfg.ParamPrompt {
			assert.JSONEq(t, `"a red bicycle"`, string(r.comfy.Graph()[a.Node].Inputs[a.Input]), "the prompt went to the assigned input")
			prompt = a.Node
		}
	}
	assert.NotEmpty(t, prompt)

	// The record belongs to this process; one an earlier process left running is a failed run, and an unknown or
	// malformed id is not found (it is a directory name).
	_, err = r.svc.Run(ctx, "../etc")
	assert.ErrorIs(t, err, workflows.ErrNotFound)
	_, _, err = r.svc.RunFile(ctx, "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, workflows.ErrNotFound)
}

func TestRunCanUseAnOlderVersionFromItsSnapshot(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	edited := []byte(string(graph[:len(graph)-1]) + `,"99":{"class_type":"PreviewImage","inputs":{"images":["8",0]}}}`)
	w2, err := r.svc.Update(ctx, w.ID, workflows.UpdateInput{API: edited})
	require.NoError(t, err)
	require.Equal(t, 2, w2.Version)

	rec, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Version: 1, Prompt: "old"})
	require.NoError(t, err)
	assert.Equal(t, w.ModelID, rec.ModelID, "the run names the version it ran")
	require.Equal(t, workflows.RunSucceeded, wait(t, r, rec.ID).Status)
	assert.NotContains(t, r.comfy.Graph(), "99", "version 1 ran without the node added in version 2")

	rec, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "new"})
	require.NoError(t, err)
	assert.Equal(t, w2.ModelID, rec.ModelID)
	require.Equal(t, workflows.RunSucceeded, wait(t, r, rec.ID).Status)
	assert.Contains(t, r.comfy.Graph(), "99")

	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Version: 9, Prompt: "x"})
	assert.ErrorIs(t, err, workflows.ErrNotFound)
}

func TestRunRefusesWhatTheWorkflowCannotTakeAndReportsAFailure(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2img-sdxl")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Stills", Kind: localcfg.MediaImage, API: graph, Spec: &spec})
	require.NoError(t, err)
	var invalid *workflows.InvalidError
	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID})
	require.ErrorAs(t, err, &invalid, "a prompt is needed")
	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "x", FirstFrame: &workflows.AssetRef{ProjectID: "p", AssetID: "a"}})
	require.ErrorAs(t, err, &invalid, "an image workflow has no first frame")
	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: "missing", Prompt: "x"})
	assert.ErrorIs(t, err, workflows.ErrNotFound)

	r.comfy.FailWith = "out of memory"
	rec, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "x"})
	require.NoError(t, err)
	done := wait(t, r, rec.ID)
	assert.Equal(t, workflows.RunFailed, done.Status)
	assert.Contains(t, done.Error, "out of memory")
	_, _, err = r.svc.RunFile(ctx, rec.ID)
	assert.ErrorIs(t, err, workflows.ErrNotFound, "a failed run has no file")
}

func TestVideoRunSnapsTheLengthAndChecksTheResolution(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	graph, spec := template(t, "txt2video-wan22-5b")
	w, err := r.svc.Create(ctx, workflows.CreateInput{Name: "Clip", Kind: localcfg.MediaVideo, API: graph, Spec: &spec})
	require.NoError(t, err)
	var invalid *workflows.InvalidError
	_, err = r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "x", Resolution: "4320p"})
	require.ErrorAs(t, err, &invalid)

	rec, err := r.svc.StartRun(ctx, workflows.RunInput{WorkflowID: w.ID, Prompt: "a clip", DurationSeconds: 4.2, Resolution: "480p"})
	require.NoError(t, err)
	done := wait(t, r, rec.ID)
	require.Equal(t, workflows.RunSucceeded, done.Status, done.Error)
	assert.Equal(t, "video/mp4", done.MimeType)
}
