package gentestrepo_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

var base = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*gentestrepo.Repository, context.Context) {
	t.Helper()
	return gentestrepo.New(testdb.Open(t)), context.Background()
}

func result(id string, pos int, model, prompt string) gentestrepo.Result {
	return gentestrepo.Result{
		ID: id, Position: pos, Media: gentestrepo.MediaImage, Model: model, APIModel: "api-" + model, Prompt: prompt, SentPrompt: prompt,
		NegativeMode: gentestrepo.NegativeNone, CostBasis: gentestrepo.CostPriced, EstimatedCost: ptr(0.04), CreatedAt: base,
	}
}

func ptr[T any](v T) *T { return &v }

func newBatch(id string, at time.Time, results ...gentestrepo.Result) gentestrepo.NewBatch {
	for i := range results {
		results[i].BatchID, results[i].CreatedAt = id, at
	}
	return gentestrepo.NewBatch{
		Batch:   gentestrepo.Batch{ID: id, Media: gentestrepo.MediaImage, Params: json.RawMessage(`{"count":1}`), EstimatedCost: 0.08, CreatedAt: at},
		Results: results,
	}
}

func TestBatchRoundTripAndProgress(t *testing.T) {
	r, ctx := setup(t)
	seed := int64(42)
	a := result("r1", 0, "flux-2", "fox")
	a.Seed, a.SeedSent, a.UsedInputs = &seed, true, []string{"i1"}
	b := result("r2", 1, "nano-banana", "fox")
	b.CostBasis, b.EstimatedCost = gentestrepo.CostUnknown, nil
	nb := newBatch("b1", base, a, b)
	nb.Inputs = []gentestrepo.Input{{ID: "i1", Role: gentestrepo.RoleReference, FilePath: "b1/in-i1.png", MimeType: "image/png", SizeBytes: 3, Origin: json.RawMessage(`{"kind":"upload"}`)}}
	nb.Prompts = []gentestrepo.PromptKey{{Prompt: "fox"}}
	require.NoError(t, r.CreateBatch(ctx, nb))
	require.NoError(t, r.SetBatchJob(ctx, "b1", "job-1"))

	got, err := r.BatchByJob(ctx, "job-1")
	require.NoError(t, err)
	assert.Equal(t, "b1", got.ID)
	assert.Equal(t, 1, got.InputCount)
	assert.Equal(t, 2, got.ResultCount)
	assert.InDelta(t, 0.08, got.EstimatedCost, 1e-9)
	_, err = r.BatchByJob(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)

	res, err := r.Results(ctx, "b1")
	require.NoError(t, err)
	require.Len(t, res, 2)
	require.NotNil(t, res[0].Seed)
	assert.Equal(t, int64(42), *res[0].Seed)
	assert.True(t, res[0].SeedSent)
	assert.Equal(t, []string{"i1"}, res[0].UsedInputs)
	assert.Nil(t, res[1].Seed, "no seed: NULL, not 0")
	assert.Nil(t, res[1].EstimatedCost)
	assert.Equal(t, gentestrepo.StatusQueued, res[1].Status)

	c, err := r.Counts(ctx, "b1")
	require.NoError(t, err)
	assert.Equal(t, gentestrepo.Counts{Queued: 2}, c)

	started, err := r.MarkRunning(ctx, "r1")
	require.NoError(t, err)
	assert.True(t, started)
	started, err = r.MarkRunning(ctx, "r1")
	require.NoError(t, err)
	assert.False(t, started, "a result that runs already is not started twice")
	require.NoError(t, r.Settle(ctx, "r1", gentestrepo.Settlement{Status: gentestrepo.StatusSucceeded, FilePath: "b1/r1.png", MimeType: "image/png", SizeBytes: 9, ActualCost: ptr(0.05), At: base.Add(time.Minute)}))
	c, _ = r.Counts(ctx, "b1")
	assert.Equal(t, gentestrepo.Counts{Queued: 1, Succeeded: 1}, c)
	assert.Equal(t, 1, c.Done())
	assert.Equal(t, 2, c.Total())

	r1, err := r.GetResult(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "b1/r1.png", r1.FilePath)
	require.NotNil(t, r1.ActualCost)
	assert.InDelta(t, 0.05, *r1.ActualCost, 1e-9)
	require.NotNil(t, r1.FinishedAt)

	err = r.Settle(ctx, "r1", gentestrepo.Settlement{Status: gentestrepo.StatusFailed, Error: "late", At: base})
	assert.ErrorIs(t, err, repository.ErrNotFound, "a result that is over is not settled again")
	r1, _ = r.GetResult(ctx, "r1")
	assert.Equal(t, gentestrepo.StatusSucceeded, r1.Status)
}

func TestCancelQueuedLeavesRunningAlone(t *testing.T) {
	r, ctx := setup(t)
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "m", "p"), result("r2", 1, "m", "p"), result("r3", 2, "m", "p"))))
	_, err := r.MarkRunning(ctx, "r1")
	require.NoError(t, err)

	n, err := r.CancelQueued(ctx, "b1", "cancelled by the user", base.Add(time.Second))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	c, _ := r.Counts(ctx, "b1")
	assert.Equal(t, gentestrepo.Counts{Running: 1, Cancelled: 2}, c)
	r2, _ := r.GetResult(ctx, "r2")
	assert.Equal(t, "cancelled by the user", r2.Error)

	started, err := r.MarkRunning(ctx, "r2")
	require.NoError(t, err)
	assert.False(t, started, "a cancelled result is never started")
	require.NoError(t, r.Settle(ctx, "r1", gentestrepo.Settlement{Status: gentestrepo.StatusSucceeded, FilePath: "f", At: base}))
}

func TestFailUnfinishedSettlesWhatTheCrashLeft(t *testing.T) {
	r, ctx := setup(t)
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "m", "p"), result("r2", 1, "m", "p"), result("r3", 2, "m", "p"), result("r4", 3, "m", "p"))))
	require.NoError(t, r.CreateBatch(ctx, newBatch("b2", base.Add(time.Hour), result("r5", 0, "m", "p"))))
	_, _ = r.MarkRunning(ctx, "r1")
	_, _ = r.MarkRunning(ctx, "r2")
	require.NoError(t, r.Settle(ctx, "r2", gentestrepo.Settlement{Status: gentestrepo.StatusSucceeded, FilePath: "kept", At: base}))
	require.NoError(t, r.Settle(ctx, "r4", gentestrepo.Settlement{Status: gentestrepo.StatusFailed, Error: "vendor said no", At: base}))
	require.NoError(t, r.FinishBatch(ctx, "b2", base.Add(2*time.Hour)))

	n, err := r.FailUnfinished(ctx, "interrupted by shutdown", base.Add(3*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 3, n, "r1 (running), r3 and r5 (queued)")

	want := map[string][2]string{
		"r1": {gentestrepo.StatusFailed, "interrupted by shutdown"}, "r2": {gentestrepo.StatusSucceeded, ""}, "r3": {gentestrepo.StatusFailed, "interrupted by shutdown"},
		"r4": {gentestrepo.StatusFailed, "vendor said no"}, "r5": {gentestrepo.StatusFailed, "interrupted by shutdown"},
	}
	for id, w := range want {
		got, err := r.GetResult(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, w[0], got.Status, id)
		assert.Equal(t, w[1], got.Error, id)
	}
	b1, _ := r.GetBatch(ctx, "b1")
	require.NotNil(t, b1.FinishedAt, "the batch that was not over is ended")
	b2, _ := r.GetBatch(ctx, "b2")
	assert.Equal(t, base.Add(2*time.Hour), *b2.FinishedAt, "an ended batch keeps its time")
	n, err = r.FailUnfinished(ctx, "again", base)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestListResultsFiltersNewestFirst(t *testing.T) {
	r, ctx := setup(t)
	vid := result("v1", 0, "kling-3.0", "wave")
	vid.Media = gentestrepo.MediaVideo
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "flux-2", "fox"), result("r2", 1, "nano-banana", "fox"))))
	require.NoError(t, r.CreateBatch(ctx, newBatch("b2", base.Add(time.Hour), vid)))
	require.NoError(t, r.CreateBatch(ctx, newBatch("b3", base.Add(2*time.Hour), result("r3", 0, "flux-2", "cat"))))
	for _, id := range []string{"r1", "v1"} {
		require.NoError(t, r.Settle(ctx, id, gentestrepo.Settlement{Status: gentestrepo.StatusSucceeded, FilePath: id, At: base}))
	}
	require.NoError(t, r.SetFavorite(ctx, "r1", true))
	require.NoError(t, r.SetFavorite(ctx, "r3", true))
	require.NoError(t, r.SetFavorite(ctx, "r3", false))
	assert.ErrorIs(t, r.SetFavorite(ctx, "nope", true), repository.ErrNotFound)

	ids := func(f gentestrepo.Filter) []string {
		list, err := r.ListResults(ctx, f)
		require.NoError(t, err)
		out := []string{}
		for _, x := range list {
			out = append(out, x.ID)
		}
		return out
	}
	assert.Equal(t, []string{"r3", "v1", "r1", "r2"}, ids(gentestrepo.Filter{}), "newest batch first, planned order inside a batch")
	assert.Equal(t, []string{"r3", "r1"}, ids(gentestrepo.Filter{Model: "flux-2"}))
	assert.Equal(t, []string{"v1"}, ids(gentestrepo.Filter{Media: gentestrepo.MediaVideo}))
	assert.Equal(t, []string{"r1"}, ids(gentestrepo.Filter{FavoriteOnly: true}))
	assert.Equal(t, []string{"v1", "r1"}, ids(gentestrepo.Filter{SucceededOnly: true}))
	assert.Equal(t, []string{"r1", "r2"}, ids(gentestrepo.Filter{BatchID: "b1"}))
	assert.Equal(t, []string{"v1"}, ids(gentestrepo.Filter{Limit: 1, Offset: 1}))
	assert.Equal(t, []string{"r3", "r2"}, ids(gentestrepo.Filter{Status: gentestrepo.StatusQueued}))
	models, err := r.Models(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"flux-2", "kling-3.0", "nano-banana"}, models)

	got, _ := r.GetResult(ctx, "r1")
	assert.True(t, got.Favorite)
}

func TestDeleteResultRemovesTheBatchWithItsLastResult(t *testing.T) {
	r, ctx := setup(t)
	nb := newBatch("b1", base, result("r1", 0, "m", "p"), result("r2", 1, "m", "p"))
	nb.Inputs = []gentestrepo.Input{{ID: "i1", Role: gentestrepo.RoleReference, FilePath: "f", MimeType: "image/png"}}
	require.NoError(t, r.CreateBatch(ctx, nb))
	_, err := r.AddImport(ctx, gentestrepo.Import{ResultID: "r1", ProjectID: "p1", AssetID: "a1", Target: json.RawMessage(`{"as":"asset"}`)})
	require.NoError(t, err)

	batch, left, err := r.DeleteResult(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "b1", batch)
	assert.Equal(t, 1, left)
	imports, err := r.Imports(ctx, "r1")
	require.NoError(t, err)
	assert.Empty(t, imports, "the import record goes with its result")
	_, left, err = r.DeleteResult(ctx, "r2")
	require.NoError(t, err)
	assert.Zero(t, left)
	_, err = r.GetBatch(ctx, "b1")
	assert.ErrorIs(t, err, repository.ErrNotFound)
	inputs, _ := r.Inputs(ctx, "b1")
	assert.Empty(t, inputs)
	_, _, err = r.DeleteResult(ctx, "r2")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestPromptHistoryDedupesAndIsIndependentOfResults(t *testing.T) {
	r, ctx := setup(t)
	nb := newBatch("b1", base, result("r1", 0, "m", "a fox"))
	nb.Prompts = []gentestrepo.PromptKey{{Prompt: "a fox"}, {Prompt: "a fox", NegativePrompt: "blur"}}
	require.NoError(t, r.CreateBatch(ctx, nb))
	nb2 := newBatch("b2", base.Add(time.Hour), result("r2", 0, "m", "a fox"))
	nb2.Prompts = []gentestrepo.PromptKey{{Prompt: "a fox"}}
	require.NoError(t, r.CreateBatch(ctx, nb2))

	list, err := r.Prompts(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2, "the same (prompt, negative prompt) is one entry")
	assert.Equal(t, "a fox", list[0].Prompt, "the one used last comes first")
	assert.Empty(t, list[0].NegativePrompt)
	assert.Equal(t, 2, list[0].UseCount)
	assert.Equal(t, base.Add(time.Hour), list[0].LastUsedAt)
	assert.Equal(t, 1, list[1].UseCount)
	assert.Equal(t, "blur", list[1].NegativePrompt)

	_, _, err = r.DeleteResult(ctx, "r1")
	require.NoError(t, err)
	require.NoError(t, r.DeleteBatch(ctx, "b2"))
	list, _ = r.Prompts(ctx)
	assert.Len(t, list, 2, "deleting results and batches never deletes a prompt")

	require.NoError(t, r.DeletePrompt(ctx, list[0].ID))
	list, _ = r.Prompts(ctx)
	assert.Len(t, list, 1)
	assert.ErrorIs(t, r.DeletePrompt(ctx, "nope"), repository.ErrNotFound)
	_, err = r.GetPrompt(ctx, "nope")
	assert.ErrorIs(t, err, repository.ErrNotFound)
}

func TestSpendCountsFilesAndLeavesUnknownPricesOut(t *testing.T) {
	r, ctx := setup(t)
	free := result("r3", 2, "local", "p")
	free.CostBasis, free.EstimatedCost = gentestrepo.CostFree, ptr(0.0)
	unknown := result("r4", 3, "chatgpt", "p")
	unknown.CostBasis, unknown.EstimatedCost = gentestrepo.CostUnknown, nil
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "a", "p"), result("r2", 1, "b", "p"), free, unknown, result("r5", 4, "c", "p"))))
	for _, id := range []string{"r1", "r2", "r3", "r4"} {
		require.NoError(t, r.Settle(ctx, id, gentestrepo.Settlement{Status: gentestrepo.StatusSucceeded, FilePath: id, At: base}))
	}
	require.NoError(t, r.Settle(ctx, "r5", gentestrepo.Settlement{Status: gentestrepo.StatusFailed, Error: "x", At: base}))

	s, err := r.Spend(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, s.Items, "only the results that made a file")
	assert.InDelta(t, 0.08, s.PricedUSD, 1e-9)
	assert.Equal(t, 1, s.UnknownItems)
}

func TestImportRecordsPlainIdsAndNeedsTheResult(t *testing.T) {
	r, ctx := setup(t)
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "m", "p"))))
	_, err := r.AddImport(ctx, gentestrepo.Import{ResultID: "nope", ProjectID: "p", AssetID: "a"})
	assert.ErrorIs(t, err, repository.ErrNotFound)

	im, err := r.AddImport(ctx, gentestrepo.Import{
		ResultID: "r1", ProjectID: "project-that-does-not-exist", AssetID: "asset-that-does-not-exist", ReferenceItemID: "item-1",
		Target: json.RawMessage(`{"as":"reference","kind":"character"}`),
	})
	require.NoError(t, err, "no foreign key to the project tables")
	assert.NotEmpty(t, im.ID)
	list, err := r.Imports(ctx, "r1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "item-1", list[0].ReferenceItemID)
	assert.JSONEq(t, `{"as":"reference","kind":"character"}`, string(list[0].Target))
}
