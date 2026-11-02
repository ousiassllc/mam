package match_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

func TestCancelStopsTheMatchBeforeTheNextRound(t *testing.T) {
	r := newRig(t, []int{40, 50, 60})
	hold, reached := make(chan struct{}), make(chan struct{}, 1)
	r.LLM.Before = func(kind string) {
		if kind == gentesttest.CallCompare && r.LLM.Count(gentesttest.CallCompare) == 0 {
			reached <- struct{}{}
			<-hold
		}
	}
	req := r.request(match.ModeAuto)
	req.MaxRounds = 4
	ref := r.start(req)
	<-reached // the first picture is made and is being measured

	res, err := r.Svc.Cancel(context.Background(), ref.BatchID)
	require.NoError(t, err)
	assert.Equal(t, 3, res.Cancelled, "the rounds that have not started are cancelled")
	close(hold)

	v := r.finished(ref.BatchID)
	assert.Equal(t, match.StopCancelled, v.Stop)
	assert.Equal(t, []int{40}, scores(v), "the round that was on its way is kept")
	assert.Len(t, r.Fake.CallsOf(domain.ModelFlux2), 1, "no picture was made after the cancel")
	assert.Zero(t, r.LLM.Count(gentesttest.CallRevise), "and no prompt was fixed")
	st, err := r.Svc.Batch(context.Background(), ref.BatchID, false)
	require.NoError(t, err)
	assert.Equal(t, gentest.StateCancelled, st.State)
	for _, rd := range v.Rounds[1:] {
		assert.Equal(t, gentestrepo.StatusCancelled, rd.Status)
	}
}

func TestAFailedRoundStopsTheMatchWithItsReason(t *testing.T) {
	for name, tc := range map[string]struct {
		kind   string
		err    error
		reason string
	}{
		"no key to write the prompt": {gentesttest.CallDescribe, llm.ErrNoAPIKey, "API キー"},
		"unusable fix":               {gentesttest.CallRevise, llm.ErrInvalidOutput, "使えない"},
		"same prompt":                {gentesttest.CallRevise, imagematch.ErrNotRevised, "これ以上直せません"},
		"timeout":                    {gentesttest.CallDescribe, llm.ErrTimeout, "時間内"},
	} {
		r := newRig(t, []int{10, 20})
		r.LLM.Err[tc.kind] = tc.err
		v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
		assert.Equal(t, match.StopError, v.Stop, name)
		assert.Contains(t, v.Error, tc.reason, name)
		last := v.Rounds[len(v.Rounds)-1]
		assert.Equal(t, gentestrepo.StatusFailed, last.Status, name)
		assert.Contains(t, last.Error, tc.reason, name)
		assert.Len(t, v.Rounds, 1+btoi(tc.kind == gentesttest.CallRevise), "the rounds that were not needed are not kept: %s", name)
		st, err := r.Svc.Batch(context.Background(), v.BatchID, false)
		require.NoError(t, err)
		if tc.kind == gentesttest.CallDescribe {
			assert.Equal(t, gentest.StateFailed, st.State, name)
			assert.Empty(t, r.Fake.Calls(), "no picture is paid for when the prompt could not be written: %s", name)
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestAFailedComparisonKeepsThePictureAndSaysItWasNotMeasured(t *testing.T) {
	r := newRig(t, nil)
	r.LLM.Err[gentesttest.CallCompare] = llm.ErrNoAPIKey
	v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
	assert.Equal(t, match.StopError, v.Stop)
	require.Len(t, v.Rounds, 1)
	r1 := v.Rounds[0]
	assert.Equal(t, gentestrepo.StatusSucceeded, r1.Status, "the picture was paid for: it stays")
	assert.True(t, r1.HasFile)
	assert.Nil(t, r1.Score)
	assert.Contains(t, r1.Error, "似てる度を測れませんでした")
	assert.Zero(t, v.Best, "nothing was measured, so there is no best round")
}

func TestAFailedPictureStopsTheMatch(t *testing.T) {
	r := newRig(t, []int{10})
	r.Fake.FailWith(domain.ModelFlux2, errors.New("vendor down"))
	v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
	assert.Equal(t, match.StopError, v.Stop)
	require.Len(t, v.Rounds, 1)
	assert.Equal(t, gentestrepo.StatusFailed, v.Rounds[0].Status)
	assert.Equal(t, "prompt 1", v.Rounds[0].Prompt, "the prompt that was sent is kept")
	assert.Zero(t, r.LLM.Count(gentesttest.CallCompare))
}

func TestViewIsRefusedForABatchThatIsNotAMatch(t *testing.T) {
	r := newRig(t, nil)
	ref, err := r.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	assert.Equal(t, gentest.StateSucceeded, st.State, "a batch of the generation test still runs as it did")

	_, err = r.M.View(context.Background(), ref.BatchID)
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
	_, err = r.M.View(context.Background(), "nope")
	assert.ErrorIs(t, err, gterr.ErrNotFound)
	m, running, err := r.M.IsMatch(context.Background(), ref.BatchID)
	require.NoError(t, err)
	assert.False(t, m)
	assert.False(t, running)
	assert.Same(t, r.M, match.From(r.Svc))
	assert.Nil(t, match.From(nil))
}

func TestTheStagedReferenceIsConsumedAndAnAssetOfTheHistoryCanBeTheReference(t *testing.T) {
	r := newRig(t, []int{90})
	req := r.request(match.ModeOnce)
	v := r.finished(r.start(req).BatchID)
	_, _, err := r.Svc.StagedFile(req.Reference.ID)
	assert.ErrorIs(t, err, gterr.ErrNotFound, "the upload was taken by the match")

	// the picture of a round of the history is an image like any other
	again := match.Request{Mode: match.ModeOnce, Reference: gentest.RefInput{Source: gentest.FromHistory, ID: v.Rounds[0].ResultID}, Model: "flux-2"}
	v2 := r.finished(r.start(again).BatchID)
	assert.Equal(t, 1, len(v2.Rounds))
}
