package gentest_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/runner"
)

func TestListModelsOffersEveryBuiltInModelWithItsCapabilities(t *testing.T) {
	r := newRig(t, gentesttest.WithoutBackend(domain.ModelNanoBananaLite))
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
	assert.False(t, by["nano-banana-lite"].Available, "an image model without a backend says why it cannot be chosen")
	assert.NotEmpty(t, by["nano-banana-lite"].UnavailableMessage)
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

	var res runner.CancelResult
	r.CallOK("cancel_generation_test", map[string]any{"batch_id": out.BatchID}, &res)
	assert.Equal(t, runner.CancelResult{Cancelled: 2, Running: 1}, res)
	release <- struct{}{}
	st := r.job(out.JobID)
	assert.Equal(t, mcpkit.JobCancelled, st.State)
	assert.Equal(t, 3, st.Done)
	assert.Contains(t, r.CallError("cancel_generation_test", map[string]any{"batch_id": out.BatchID}), "conflict", "a finished batch")
	assert.Contains(t, r.CallError("cancel_generation_test", map[string]any{"batch_id": "nope"}), "not_found")
}

func TestARunningBatchShowsTheStepsOfItsComfyUICallInBothTools(t *testing.T) {
	r := newRig(t)
	release := r.GT.Fake.Hold(domain.ModelFlux2)
	args := map[string]any{"media": "image", "models": []string{"flux-2"}, "count": 1, "prompts": []map[string]any{{"prompt": "fox"}}}
	out, _ := r.run(args)
	<-r.GT.Fake.Started()

	var b mcpgentest.BatchOutput
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	assert.Nil(t, b.Progress, "a cloud model has no step: the field is left out")
	var st mcpkit.JobStatus
	r.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
	assert.Nil(t, st.Steps)

	call := live.Default.Start("test-ep", "prompt-"+out.JobID)
	defer call.End()
	call.Own(out.JobID)
	call.Step("3", "KSampler", 8, 20, 40)

	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.NotNil(t, b.Progress)
	assert.Equal(t, 8, b.Progress.Step)
	assert.Equal(t, 20, b.Progress.MaxStep)
	assert.Equal(t, 40, b.Progress.Percent)
	r.CallOK("get_job", map[string]any{"job_id": out.JobID}, &st)
	require.NotNil(t, st.Steps)
	assert.Equal(t, 40, st.Steps.Percent)

	call.End()
	release <- struct{}{}
	st = r.job(out.JobID)
	assert.Equal(t, mcpkit.JobSucceeded, st.State)
	assert.Nil(t, st.Steps, "a finished batch has no step")
	var done mcpgentest.BatchOutput
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &done)
	assert.Nil(t, done.Progress)
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

// #928: every model carries whether it may be used commercially (with the license, the source and the day it was read), and the list
// can be narrowed by it; the narrowed list is what the setting 商用OKのモデルだけを使う starts from.
func TestListModelsSaysWhetherEachModelMayBeUsedCommercially(t *testing.T) {
	r := newRig(t)
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{}, &out)

	assert.Equal(t, "all", out.CommercialFilter)
	by := map[string]modelcatalog.Info{}
	for _, m := range out.Models {
		assert.NoError(t, m.License.Check(), m.ID)
		by[m.ID] = m
	}
	assert.Equal(t, modellicense.Yes, by["flux-2"].License.Commercial)
	assert.Equal(t, modellicense.Conditional, by["luma-ray-3.2"].License.Commercial)
	assert.NotEmpty(t, by["luma-ray-3.2"].License.Condition)
	assert.Equal(t, modellicense.Yes, by["kling-3.0"].License.Commercial, "#969: Kling's API terms (§6.4) do not restrict commercial use")
	assert.Equal(t, "https://openai.com/policies/services-agreement/", by["gpt-image-2"].License.SourceURL, "the API is under the services agreement, not the consumer terms of use")
}

func TestListModelsNarrowsByTheCommercialArgument(t *testing.T) {
	r := newRig(t)

	var ok mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "video", "commercial": "ok"}, &ok)
	assert.Equal(t, "ok", ok.CommercialFilter)
	require.NotEmpty(t, ok.Models)
	for _, m := range ok.Models {
		assert.Equal(t, modellicense.Yes, m.License.Commercial, m.ID)
		assert.NotEqual(t, "luma-ray-3.2", m.ID, "条件つき is out of 商用OKのみ")
	}

	var cond mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "video", "commercial": "ok_or_conditional"}, &cond)
	ids := map[string]bool{}
	for _, m := range cond.Models {
		ids[m.ID] = true
		assert.NotEqual(t, modellicense.Unknown, m.License.Commercial, m.ID)
	}
	assert.True(t, ids["luma-ray-3.2"], "条件つき is in")
	assert.True(t, ids["kling-3.0"], "商用OK is in")

	assert.Contains(t, r.CallError("list_generation_models", map[string]any{"commercial": "yes"}), "validation")
}

func TestListModelsStartsAtTheSettingWhenNoCommercialArgumentIsGiven(t *testing.T) {
	filter := modellicense.FilterOK
	modellicense.SetDefaultFilter(func() modellicense.Filter { return filter })
	t.Cleanup(func() { modellicense.SetDefaultFilter(nil) })
	r := newRig(t)

	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{}, &out)
	assert.Equal(t, "ok", out.CommercialFilter, "商用OKのモデルだけを使う is on")
	for _, m := range out.Models {
		assert.Equal(t, modellicense.Yes, m.License.Commercial, m.ID)
	}

	var widened mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"commercial": "all"}, &widened)
	assert.Greater(t, len(widened.Models), len(out.Models), "an explicit commercial argument overrides the setting")
}
