// Package batchinputtest checks the input of a batch that comes from an earlier batch (gentest.FromBatchInput, Issue #977): the copy the batch kept of an
// uploaded picture. It is a package of its own because servicetest is at its line limit.
package batchinputtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

func imageReq(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
}

// An uploaded input is kept with its batch and can be an input again (FromBatchInput), as long as one result of the batch is there: the redo of a result that
// was made from an upload ("この設定でもう一度", #977) uses it. The new result records where its picture came from.
func TestAnUploadedInputOfABatchIsAnInputAgainUntilTheBatchIsDeleted(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	staged, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	upload := imageReq("nano-banana")
	upload.References = []gentest.RefInput{{Source: gentest.FromStaged, ID: staged}}
	run, err := r.Confirmed(upload)
	require.NoError(t, err)
	first := r.Wait(run.BatchID)
	d, err := r.Svc.Result(ctx, first.Results[0].ID)
	require.NoError(t, err)
	require.Len(t, d.Inputs, 1)
	assert.Equal(t, gentest.FromStaged, d.Inputs[0].Origin.Kind)

	again := imageReq("nano-banana")
	again.References = []gentest.RefInput{{Source: gentest.FromBatchInput, ID: d.Inputs[0].ID}}
	run, err = r.Confirmed(again)
	require.NoError(t, err)
	second := r.Wait(run.BatchID)
	require.Equal(t, gentest.StateSucceeded, second.State)
	d2, err := r.Svc.Result(ctx, second.Results[0].ID)
	require.NoError(t, err)
	assert.Equal(t, gentest.FromBatchInput, d2.Inputs[0].Origin.Kind, "the origin is recorded")
	assert.Equal(t, d.Inputs[0].ID, d2.Inputs[0].Origin.ID)
	calls := r.Fake.CallsOf(domain.ModelNanoBanana)
	require.Len(t, calls, 2)
	assert.Equal(t, calls[0].Image.Inputs[0].Data, calls[1].Image.Inputs[0].Data, "the same picture went to the model")

	require.NoError(t, r.Svc.DeleteResult(ctx, first.Results[0].ID))
	gone := imageReq("nano-banana")
	gone.References = []gentest.RefInput{{Source: gentest.FromBatchInput, ID: d.Inputs[0].ID}}
	var invalid *gterr.InvalidError
	_, err = r.Svc.Estimate(ctx, gone)
	assert.True(t, errors.As(err, &invalid), "the copy goes with the last result of its batch: refused at the estimate")
	assert.Len(t, r.Fake.CallsOf(domain.ModelNanoBanana), 2)
}
