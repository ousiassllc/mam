package gentest_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

func TestListModelsOffersEveryBuiltInModelWithItsCapabilities(t *testing.T) {
	r := newRig(t, gentesttest.WithoutBackend(domain.ModelImagen4))
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{}, &out)
	by := map[string]modelcatalog.Info{}
	for _, m := range out.Models {
		by[m.ID] = m
	}
	require.Contains(t, by, "flux-2")
	assert.True(t, by["flux-2"].SupportsSeed)
	assert.True(t, by["flux-2"].Available)
	assert.False(t, by["nano-banana"].SupportsSeed)
	require.Contains(t, by, "runway-gen-4.5")
	assert.NotEmpty(t, by["runway-gen-4.5"].Video.Resolutions)
	assert.False(t, by["imagen-4"].Available, "an image model without a backend says why it cannot be chosen")
	assert.NotEmpty(t, by["imagen-4"].UnavailableMessage)
	assert.Equal(t, 50, out.Limits.MaxItems)
	assert.InDelta(t, 20, out.Limits.MaxCostUSD, 1e-9)

	var videos mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "video"}, &videos)
	for _, m := range videos.Models {
		assert.Equal(t, "video", m.Media)
	}
}

func TestEstimateThenRunAsAJobWhoseResultsAreReadAndDownloaded(t *testing.T) {
	r := newRig(t)
	out, est := r.run(batch("flux-2", "nano-banana"))
	assert.Equal(t, 8, est.TotalItems, "2 models x 2 prompts x 2")
	require.Len(t, est.Breakdown, 2)
	assert.Equal(t, 8, out.TotalItems)
	assert.Equal(t, mcpkit.JobQueued, out.State)
	require.NotEmpty(t, out.BatchID)

	st := r.job(out.JobID)
	assert.Equal(t, "gentest", st.Kind)
	require.Equal(t, mcpkit.JobSucceeded, st.State, st.Error)
	assert.Equal(t, 8, st.Done)
	assert.Equal(t, 8, st.Total)
	assert.Equal(t, out.BatchID, st.Result["batch_id"])

	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	assert.True(t, b.Finished)
	require.Len(t, b.Results, 8)
	var folded int
	for _, x := range b.Results {
		assert.Equal(t, "succeeded", x.Status)
		if x.Model == "flux-2" {
			assert.True(t, x.SeedSent, "FLUX.2 takes a seed")
		}
		if x.NegativeMode == "folded" {
			folded++
			assert.Contains(t, x.SentPrompt, "a red fox")
			assert.Contains(t, x.SentPrompt, "blurry")
		}
	}
	assert.Positive(t, folded, "a model without a negative field gets the negative prompt in the prompt")

	var got mcpgentest.ResultOutput
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
	require.NotNil(t, got.File)
	assert.Contains(t, got.File.URL, mcpkit.FilesPath, "a token URL, not bytes")
	dl := r.serve(http.MethodGet, got.File.URL, nil, "")
	require.Equal(t, http.StatusOK, dl.Code)
	assert.Equal(t, gentesttest.PNG, dl.Body.Bytes())
	assert.Equal(t, "image/png", dl.Header().Get("Content-Type"))

	// Nothing of a generation test is a Take, a Cost Ledger row or an asset.
	for _, table := range []string{"take", "cost_ledger_entry", "asset"} {
		var n int
		require.NoError(t, r.DB.Reader().QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
		assert.Zero(t, n, table)
	}
}

func TestRunNeedsTheConfirmedEstimateAndRefusesAChangedOne(t *testing.T) {
	r := newRig(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		one, cap := 4, 0.5
		c.Gentest.MaxItems, c.Gentest.MaxCostUSD = &one, &cap
	}))
	args := batch("nano-banana")
	args["count"] = 1
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)

	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range args {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	assert.Contains(t, r.CallError("run_generation_test", args), "estimate_id", "no estimate_id: the batch was never confirmed")
	assert.Contains(t, r.CallError("run_generation_test", with(map[string]any{"estimate_id": "0000", "confirmed_total_usd": est.TotalUSD})), "conflict")
	assert.Contains(t, r.CallError("run_generation_test", with(map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD + 1})), "conflict")
	changed := with(map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD, "count": 2})
	assert.Contains(t, r.CallError("run_generation_test", changed), "conflict", "the request is not the one that was confirmed")
	assert.Empty(t, r.GT.Fake.Calls(), "nothing ran")

	over := batch("nano-banana")
	over["count"] = 3
	assert.Contains(t, r.CallError("estimate_generation_test", over), "validation", "more results than max_items")
}

func TestRunRefusesABatchOverTheCostCap(t *testing.T) {
	r := newRig(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		cap := 0.01
		c.Gentest.MaxCostUSD = &cap
	}))
	args := batch("flux-2")
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	assert.NotEmpty(t, est.OverLimit)
	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	assert.Contains(t, r.CallError("run_generation_test", run), "conflict")
	assert.Empty(t, r.GT.Fake.Calls())
}

func TestCancelStopsWhatHasNotStarted(t *testing.T) {
	r := newRig(t, gentesttest.WithConfig(func(c *modeladapter.Config) {
		v := c.Vendors[modeladapter.VendorFlux]
		v.MaxConcurrency = 1
		c.Vendors[modeladapter.VendorFlux] = v
	}))
	release := r.GT.Fake.Hold(domain.ModelFlux2)
	args := map[string]any{"media": "image", "models": []string{"flux-2"}, "count": 3, "prompts": []map[string]any{{"prompt": "fox"}}}
	out, _ := r.run(args)
	<-r.GT.Fake.Started()

	var res gentest.CancelResult
	r.CallOK("cancel_generation_test", map[string]any{"batch_id": out.BatchID}, &res)
	assert.Equal(t, gentest.CancelResult{Cancelled: 2, Running: 1}, res)
	release <- struct{}{}
	st := r.job(out.JobID)
	assert.Equal(t, mcpkit.JobCancelled, st.State)
	assert.Equal(t, 3, st.Done)
	assert.Contains(t, r.CallError("cancel_generation_test", map[string]any{"batch_id": out.BatchID}), "conflict", "a finished batch")
	assert.Contains(t, r.CallError("cancel_generation_test", map[string]any{"batch_id": "nope"}), "not_found")
}

func TestHistoryFavoritesDeletionAndPrompts(t *testing.T) {
	r := newRig(t)
	out, _ := r.run(batch("flux-2"))
	r.job(out.JobID)
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	id := b.Results[0].ID

	var fav mcpgentest.FavoriteOutput
	r.CallOK("set_generation_favorite", map[string]any{"result_id": id, "favorite": true}, &fav)
	assert.True(t, fav.Favorite)
	var list mcpgentest.ListResultsOutput
	r.CallOK("list_generation_results", map[string]any{"favorite": true}, &list)
	require.Len(t, list.Results, 1)
	assert.Equal(t, id, list.Results[0].ID)
	assert.Equal(t, 4, list.Spend.Items, "the total is of the whole history, not of the filter")
	r.CallOK("list_generation_results", map[string]any{"model": "nano-banana"}, &list)
	assert.Empty(t, list.Results)
	assert.Contains(t, r.CallError("list_generation_results", map[string]any{"media": "audio"}), "validation")

	var prompts mcpgentest.ListPromptsOutput
	r.CallOK("list_generation_prompts", map[string]any{}, &prompts)
	require.Len(t, prompts.Prompts, 2, "two prompts, however many results")
	var del mcpgentest.DeletedOutput
	r.CallOK("delete_generation_prompt", map[string]any{"prompt_id": prompts.Prompts[0].ID}, &del)
	assert.True(t, del.Deleted)
	r.CallOK("list_generation_prompts", map[string]any{}, &prompts)
	assert.Len(t, prompts.Prompts, 1)
	r.CallOK("list_generation_results", map[string]any{}, &list)
	assert.Len(t, list.Results, 4, "deleting a prompt keeps the results")

	r.CallOK("delete_generation_result", map[string]any{"result_id": id}, &del)
	assert.True(t, del.Deleted)
	assert.Contains(t, r.CallError("get_generation_result", map[string]any{"result_id": id}), "not_found")
	assert.Contains(t, r.CallError("delete_generation_result", map[string]any{"result_id": id}), "not_found")
	assert.Contains(t, r.CallError("set_generation_favorite", map[string]any{"result_id": id, "favorite": true}), "not_found")
}
