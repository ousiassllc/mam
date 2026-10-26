// Package localtest runs the generation test over local ComfyUI models (the model manager's rig and the fake ComfyUI server of
// the Model Adapter), from the request to the stored file: what the model manager's trial panel does.
package localtest_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

var ctx = context.Background

// box registers the fake ComfyUI as the endpoint "box" with an image model "t2i", one that needs a reference image ("img") and a
// video model "vid".
func box(t *testing.T, r *mmtest.Rig) *comfyuitest.Fake {
	t.Helper()
	f := comfyuitest.New(t)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	for name, tpl := range map[string]string{"img": "img2img-sdxl", "vid": "img2video-wan22-5b", "t2i": "txt2img-sdxl"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(t, ok)
		require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: name, Model: tm.Model}))
	}
	return f
}

// run prices the request, runs it with that estimate and waits for the batch to end.
func run(t *testing.T, r *mmtest.Rig, req gentest.Request) gentest.BatchStatus {
	t.Helper()
	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	ref, err := r.Gentest.Run(ctx(), req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.NoError(t, err)
	r.Start()
	var st gentest.BatchStatus
	require.Eventually(t, func() bool {
		st, err = r.Gentest.Batch(ctx(), ref.BatchID, true)
		return err == nil && st.Finished
	}, 30*time.Second, 20*time.Millisecond, "the batch did not end")
	return st
}

func prompt(p string) []gentest.PromptInput { return []gentest.PromptInput{{Prompt: p}} }

func TestAnImageBatchOnALocalModelIsFreeSendsTheSeedAndTheReferenceAndKeepsTheFile(t *testing.T) {
	r := mmtest.New(t)
	f := box(t, r)
	staged, _, err := r.Gentest.StageReference(comfyuitest.PNG)
	require.NoError(t, err)

	req := gentest.Request{
		Media: "image", Models: []string{"comfyui:box/img"}, Prompts: []gentest.PromptInput{{Prompt: "a red fox", Negative: "blurry"}},
		AspectRatio: "16:9", References: []gentest.RefInput{{Source: gentest.FromStaged, ID: staged}},
	}
	est, err := r.Gentest.Estimate(ctx(), req)
	require.NoError(t, err)
	assert.Zero(t, est.TotalUSD)
	assert.Equal(t, "free", est.Breakdown[0].Basis, "a local model has no bill")

	st := run(t, r, req)
	require.Equal(t, gentest.StateSucceeded, st.State)
	res := st.Results[0]
	assert.True(t, res.SeedSent, "the seed goes through the workflow")
	assert.Equal(t, "field", res.NegativeMode, "a workflow with a negative prompt node takes it as a field")
	assert.Equal(t, 1, f.Count("POST /upload/image"), "the reference image reached the server")

	file, err := r.Gentest.ResultFile(ctx(), res.ID)
	require.NoError(t, err)
	got, err := os.ReadFile(file.Path)
	require.NoError(t, err)
	assert.Equal(t, comfyuitest.PNG, got)
	assert.Equal(t, "image/png", file.Mime)
}

func TestAVideoBatchUsesTheModelsDefaultsAndTheFirstFrame(t *testing.T) {
	r := mmtest.New(t)
	f := box(t, r)
	staged, _, err := r.Gentest.StageReference(comfyuitest.PNG)
	require.NoError(t, err)

	st := run(t, r, gentest.Request{
		Media: "video", Models: []string{"comfyui:box/vid"}, Prompts: prompt("waves"),
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: staged, Role: "first_frame"}},
	})
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	res := st.Results[0]
	assert.Equal(t, "720p", res.Resolution, "the default resolution, when the model offers it")
	assert.Equal(t, 5.0, res.DurationSeconds, "the default length, snapped to what the model makes")
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.Equal(t, 1, f.Count("POST /upload/image"), "as the first frame")
}

func TestARequestIsCheckedAgainstTheModelBeforeItIsQueued(t *testing.T) {
	r := mmtest.New(t)
	f := box(t, r)
	staged, _, err := r.Gentest.StageReference(comfyuitest.PNG)
	require.NoError(t, err)
	var invalid *gterr.InvalidError
	for name, req := range map[string]gentest.Request{
		"no prompt":             {Media: "image", Models: []string{"comfyui:box/t2i"}},
		"unknown model":         {Media: "image", Models: []string{"comfyui:box/missing"}, Prompts: prompt("x")},
		"video as image":        {Media: "image", Models: []string{"comfyui:box/vid"}, Prompts: prompt("x")},
		"aspect":                {Media: "image", Models: []string{"comfyui:box/t2i"}, Prompts: prompt("x"), AspectRatio: "wide"},
		"staged image not here": {Media: "image", Models: []string{"comfyui:box/img"}, Prompts: prompt("x"), References: []gentest.RefInput{{Source: gentest.FromStaged, ID: "12345678.png"}}},
	} {
		_, err := r.Gentest.Estimate(ctx(), req)
		assert.ErrorAs(t, err, &invalid, name)
	}
	assert.Zero(t, f.Count("POST /prompt"))
	assert.FileExists(t, mustStaged(t, r, staged), "a refused request leaves the staged picture alone")
	batches, err := r.Gentest.Batches(ctx(), 10)
	require.NoError(t, err)
	assert.Empty(t, batches, "nothing was queued")
}

func mustStaged(t *testing.T, r *mmtest.Rig, id string) string {
	t.Helper()
	path, _, err := r.Gentest.StagedFile(id)
	require.NoError(t, err)
	return path
}

func TestAFailedResultKeepsItsReason(t *testing.T) {
	r := mmtest.New(t)
	f := box(t, r)
	f.FailWith = "CUDA out of memory"
	st := run(t, r, gentest.Request{Media: "image", Models: []string{"comfyui:box/t2i"}, Prompts: prompt("x")})
	assert.Equal(t, gentest.StateFailed, st.State)
	assert.Equal(t, "failed", st.Results[0].Status)
	assert.Contains(t, st.Results[0].Error, "CUDA out of memory")
	assert.NoError(t, r.Gentest.DeleteResult(ctx(), st.Results[0].ID), "a failed result can be deleted")
}
