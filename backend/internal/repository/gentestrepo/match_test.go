package gentestrepo_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

func TestContinueBatchReopensAFinishedBatchAndAddsQueuedRounds(t *testing.T) {
	r, ctx := setup(t)
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "flux-2", ""))))
	require.ErrorIs(t, r.ContinueBatch(ctx, "b1", []gentestrepo.Result{result("r2", 1, "flux-2", "")}, 0.04), repository.ErrNotFound,
		"a batch that is not over cannot be continued: another go-on got there first")
	require.NoError(t, r.FinishBatch(ctx, "b1", base))

	more := result("r2", 1, "flux-2", "")
	more.BatchID = "b1"
	require.NoError(t, r.ContinueBatch(ctx, "b1", []gentestrepo.Result{more}, 0.04))
	b, err := r.GetBatch(ctx, "b1")
	require.NoError(t, err)
	assert.Nil(t, b.FinishedAt, "the batch is open again")
	assert.InDelta(t, 0.12, b.EstimatedCost, 1e-9, "the estimate of the new rounds is added")
	list, err := r.Results(ctx, "b1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, gentestrepo.StatusQueued, list[1].Status)

	assert.ErrorIs(t, r.ContinueBatch(ctx, "nope", nil, 0), repository.ErrNotFound)
}

func TestDeleteQueuedRemovesOnlyTheRoundsThatNeverStarted(t *testing.T) {
	r, ctx := setup(t)
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "flux-2", ""), result("r2", 1, "flux-2", ""), result("r3", 2, "flux-2", ""))))
	started, err := r.MarkRunning(ctx, "r1", base)
	require.NoError(t, err)
	require.True(t, started)

	n, err := r.DeleteQueued(ctx, "b1")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	list, err := r.Results(ctx, "b1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "r1", list[0].ID)
}

func TestSetResultPromptWritesThePromptAndPutsItIntoThePromptHistory(t *testing.T) {
	r, ctx := setup(t)
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "flux-2", ""))))
	require.NoError(t, r.SetResultPrompt(ctx, "r1", "a red fox", json.RawMessage(`{"aspect_ratio":"1:1","match":{"round":1}}`), base))
	require.NoError(t, r.SetResultPrompt(ctx, "r1", "a red fox", nil, base.Add(1)), "empty params stay a JSON object")

	got, err := r.GetResult(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "a red fox", got.Prompt)
	assert.Equal(t, "a red fox", got.SentPrompt, "a match sends the prompt it wrote")
	assert.JSONEq(t, `{}`, string(got.Params))
	prompts, err := r.Prompts(ctx)
	require.NoError(t, err)
	require.Len(t, prompts, 1)
	assert.Equal(t, 2, prompts[0].UseCount)

	assert.ErrorIs(t, r.SetResultPrompt(ctx, "nope", "x", nil, base), repository.ErrNotFound)
}

func TestSetResultParamsReplacesTheParams(t *testing.T) {
	r, ctx := setup(t)
	require.NoError(t, r.CreateBatch(ctx, newBatch("b1", base, result("r1", 0, "flux-2", "fox"))))
	require.NoError(t, r.SetResultParams(ctx, "r1", json.RawMessage(`{"match":{"round":1,"score":70}}`)))
	got, err := r.GetResult(ctx, "r1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"match":{"round":1,"score":70}}`, string(got.Params))
	assert.ErrorIs(t, r.SetResultParams(ctx, "nope", nil), repository.ErrNotFound)
}
