// The time an estimate gives a local model (Issue #1002), through the estimate of the service.
package timing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/timing"
)

func imageReq(models ...string) gentest.Request {
	return gentest.Request{Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}}
}

func line(t *testing.T, e gentest.Estimate, model string) gentest.ModelEstimate {
	t.Helper()
	for _, l := range e.Breakdown {
		if l.Model == model {
			return l
		}
	}
	require.Failf(t, "no line", "%s in %+v", model, e.Breakdown)
	return gentest.ModelEstimate{}
}

// ran records a result of the model that ran for took with the params, as the runner leaves it: MarkRunning at start, Settle at the end.
func ran(t *testing.T, r *gentesttest.Rig, id, model, status string, params record.ResultParams, took time.Duration) {
	t.Helper()
	ctx, start := context.Background(), time.Now().Add(-time.Hour).UTC()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	nb := gentestrepo.NewBatch{Batch: gentestrepo.Batch{ID: "b-" + id, Media: "image", CreatedAt: start}, Results: []gentestrepo.Result{{
		ID: id, BatchID: "b-" + id, Media: "image", Model: model, Prompt: "p", SentPrompt: "p", NegativeMode: "none", CostBasis: "free", Params: raw, CreatedAt: start}}}
	require.NoError(t, r.Repo.CreateBatch(ctx, nb))
	started, err := r.Repo.MarkRunning(ctx, id, start)
	require.NoError(t, err)
	require.True(t, started)
	require.NoError(t, r.Repo.Settle(ctx, id, gentestrepo.Settlement{Status: status, At: start.Add(took)}))
}

func TestEstimateGivesALocalModelTheMedianOfItsPastTimesAndSaysUnknownWithoutHistory(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithNoReferenceModel())
	local := string(gentesttest.NoReferenceModel)
	req := imageReq(local, "flux-2")
	req.Count = 2
	ctx := context.Background()

	e, err := r.Svc.Estimate(ctx, req)
	require.NoError(t, err)
	l := line(t, e, local)
	assert.Equal(t, timing.Unknown, l.TimeBasis, "no result yet: nothing is guessed")
	assert.Nil(t, l.SecondsPerItem)
	assert.NotEmpty(t, l.TimeNote)
	assert.Equal(t, []string{local}, e.UnknownTimeModels)
	assert.Nil(t, e.TotalSeconds, "no model has a time")
	cloud := line(t, e, "flux-2")
	assert.Empty(t, cloud.TimeBasis, "a vendor's queue decides: a cloud model gets no time")
	assert.Nil(t, cloud.SecondsPerItem)
	before := e.ID

	same := record.ResultParams{AspectRatio: e.Request.AspectRatio}
	other := record.ResultParams{AspectRatio: "21:9"}
	for i, took := range []time.Duration{60 * time.Second, 300 * time.Second, 90 * time.Second} {
		ran(t, r, fmt.Sprintf("ok%d", i), local, gentestrepo.StatusSucceeded, same, took)
	}
	ran(t, r, "bigger", local, gentestrepo.StatusSucceeded, other, 3000*time.Second)
	ran(t, r, "failed", local, gentestrepo.StatusFailed, same, 5000*time.Second)

	e, err = r.Svc.Estimate(ctx, req)
	require.NoError(t, err)
	l = line(t, e, local)
	assert.Equal(t, timing.History, l.TimeBasis)
	require.NotNil(t, l.SecondsPerItem)
	assert.Equal(t, 90, *l.SecondsPerItem, "the median of the three that ran with the same settings; the other size and the failure do not count")
	assert.Equal(t, 3, l.TimeSamples)
	assert.Empty(t, l.TimeNote)
	assert.Empty(t, e.UnknownTimeModels)
	require.NotNil(t, e.TotalSeconds)
	assert.Equal(t, 90*2, *e.TotalSeconds, "two results of the local model")
	assert.Equal(t, before, e.ID, "the times come from the history: the confirmation of the price does not change with them")

	req.AspectRatio = "21:9"
	e, err = r.Svc.Estimate(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 3000, *line(t, e, local).SecondsPerItem, "the settings tell the results apart")
}
