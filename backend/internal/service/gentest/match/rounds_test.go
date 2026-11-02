package match_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/history"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

func TestAnAutoMatchFixesThePromptUntilTheTargetIsReachedAndKeepsEveryRound(t *testing.T) {
	r := newRig(t, []int{40, 70, 90, 95})
	ref := r.start(r.request(match.ModeAuto))
	assert.Equal(t, 5, ref.TotalItems)
	v := r.finished(ref.BatchID)

	assert.Equal(t, match.StopReached, v.Stop)
	assert.Equal(t, []int{40, 70, 90}, scores(v), "it stopped at the first round that reached 85")
	require.Len(t, v.Rounds, 3, "the two rounds that were not needed are not kept")
	assert.Equal(t, 3, v.Total)
	assert.Equal(t, 3, v.Done)
	assert.Equal(t, 3, v.Best)
	require.NotNil(t, v.BestScore)
	assert.Equal(t, 90, *v.BestScore)
	assert.Equal(t, "prompt 1, fix 1, fix 2", v.BestPrompt, "the best prompt is the one to use")
	assert.Equal(t, 85, v.Target)

	r1, r2, r3 := v.Rounds[0], v.Rounds[1], v.Rounds[2]
	assert.Equal(t, "prompt 1", r1.Prompt, "written from the reference")
	assert.Zero(t, r1.BasedOn)
	assert.Empty(t, r1.Change)
	assert.Equal(t, []string{"差 1"}, r1.Differences)
	assert.Equal(t, 1, r2.BasedOn, "the second round was fixed from the best so far: the first")
	assert.Equal(t, "prompt 1, fix 1", r2.Prompt)
	assert.Equal(t, "prompt 1", r2.BasePrompt, "the prompt before the fix is kept next to the one after")
	assert.Equal(t, "直した 1", r2.Change)
	assert.Equal(t, 2, r3.BasedOn, "the second looked best then")
	assert.True(t, r3.Best)
	assert.False(t, r1.Best)
	for i, rd := range v.Rounds {
		assert.Equal(t, i+1, rd.Round)
		assert.Equal(t, gentestrepo.StatusSucceeded, rd.Status)
		assert.True(t, rd.HasFile)
	}

	// each picture is a result of the history, made from the prompt alone
	calls := r.Fake.CallsOf(domain.ModelFlux2)
	require.Len(t, calls, 3)
	for i, c := range calls {
		assert.Equal(t, v.Rounds[i].Prompt, c.Image.Prompt)
		assert.Empty(t, c.Image.Inputs, "the reference is shown to the LLM only: giving it to the model would measure nothing")
	}
	hist, err := r.Svc.History(context.Background(), history.Filter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, hist, 3)
	assert.Equal(t, 1, r.LLM.Count(gentesttest.CallDescribe))
	assert.Equal(t, 3, r.LLM.Count(gentesttest.CallCompare))
	assert.Equal(t, 2, r.LLM.Count(gentesttest.CallRevise))
	for _, c := range r.LLM.Calls() {
		switch c.Kind {
		case gentesttest.CallDescribe:
			assert.Equal(t, llm.AgentPromptPlanner, c.Agent)
			assert.Equal(t, 1, c.Images)
		case gentesttest.CallCompare:
			assert.Equal(t, llm.AgentVideoJudge, c.Agent)
			assert.Equal(t, 2, c.Images, "the reference and the picture")
		case gentesttest.CallRevise:
			assert.Equal(t, llm.AgentPromptPlanner, c.Agent)
			assert.Equal(t, 2, c.Images)
		}
	}
	st, err := r.Svc.Batch(context.Background(), ref.BatchID, false)
	require.NoError(t, err)
	assert.Equal(t, gentest.StateSucceeded, st.State)
	assert.InDelta(t, 5*0.03, st.EstimatedUSD, 1e-9)
	d, err := r.Svc.Result(context.Background(), v.Rounds[2].ResultID)
	require.NoError(t, err)
	require.Len(t, d.Inputs, 1, "the reference image is an input of the batch: the history shows it")
	assert.Equal(t, gentest.RoleReference, d.Inputs[0].Role)
}

func TestAnAutoMatchStopsAtTheLimitAndNamesTheBestRound(t *testing.T) {
	r := newRig(t, []int{60, 30, 50})
	req := r.request(match.ModeAuto)
	req.MaxRounds = 3
	v := r.finished(r.start(req).BatchID)

	assert.Equal(t, match.StopLimit, v.Stop)
	assert.Equal(t, []int{60, 30, 50}, scores(v))
	assert.Equal(t, 1, v.Best, "the first round looks most like the reference")
	assert.Equal(t, "prompt 1", v.BestPrompt)
	assert.Equal(t, 1, v.Rounds[1].BasedOn, "a fix is built on the best round, not on the one that got worse")
	assert.Equal(t, 1, v.Rounds[2].BasedOn)
	assert.Len(t, r.Fake.CallsOf(domain.ModelFlux2), 3)
}

func TestAOnceMatchMakesOnePictureAndDoesNotFixAnything(t *testing.T) {
	r := newRig(t, []int{20})
	v := r.finished(r.start(r.request(match.ModeOnce)).BatchID)
	assert.Equal(t, match.StopLimit, v.Stop)
	assert.Equal(t, []int{20}, scores(v))
	assert.Zero(t, r.LLM.Count(gentesttest.CallRevise))
	assert.Len(t, r.Fake.Calls(), 1)
}

func TestAMatchGoesOnWithWordsOfTheUserAndKeepsThemInTheHistory(t *testing.T) {
	r := newRig(t, []int{40, 60})
	first := r.start(r.request(match.ModeAdjust))
	v := r.finished(first.BatchID)
	require.Equal(t, []int{40}, scores(v))

	req := match.Request{ContinueBatchID: first.BatchID, Mode: match.ModeAdjust, Instruction: "もう少し明るく"}
	e, err := r.M.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 1, e.Rounds)
	assert.Equal(t, "flux-2", e.Request.Model, "the model, the aspect ratio and the target stay")
	assert.Equal(t, 85, e.Request.TargetScore)
	second, err := r.M.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	require.NoError(t, err)
	assert.Equal(t, first.BatchID, second.BatchID, "the match goes on in its own batch")
	v = r.finished(first.BatchID)

	assert.Equal(t, []int{40, 60}, scores(v))
	r2 := v.Rounds[1]
	assert.Equal(t, 2, r2.Round, "the rounds go on counting")
	assert.Equal(t, "もう少し明るく", r2.Instruction, "the words of the user are in the history with the fixed prompt")
	assert.Equal(t, "prompt 1, fix 1", r2.Prompt)
	assert.Equal(t, 1, r2.BasedOn)
	assert.Equal(t, "prompt 1", r2.BasePrompt)
	revise := r.LLM.Calls()[len(r.LLM.Calls())-2]
	require.Equal(t, gentesttest.CallRevise, revise.Kind)
	assert.Contains(t, revise.User, "もう少し明るく", "the words reach the LLM, which writes the English")
	assert.NotContains(t, r2.Prompt, "もう少し明るく", "and the prompt stays English")
	st, err := r.Svc.Batch(context.Background(), first.BatchID, false)
	require.NoError(t, err)
	assert.InDelta(t, 2*0.03, st.EstimatedUSD, 1e-9, "the estimate of the go-on is added")
}

func TestTheWordsOfTheFirstRoundGoIntoThePromptWrittenForTheReference(t *testing.T) {
	r := newRig(t, []int{90})
	req := r.request(match.ModeOnce)
	req.Instruction = "背景は海に"
	r.finished(r.start(req).BatchID)
	describe := r.LLM.Calls()[0]
	require.Equal(t, gentesttest.CallDescribe, describe.Kind)
	assert.Contains(t, describe.User, "背景は海に")
}

func TestAMatchThatIsStillRunningCannotBeContinuedButTakesWordsForTheNextFix(t *testing.T) {
	r := newRig(t, []int{40, 90})
	hold, reached := make(chan struct{}), make(chan struct{}, 1)
	r.LLM.Before = func(kind string) {
		if kind == gentesttest.CallCompare && r.LLM.Count(gentesttest.CallCompare) == 0 {
			reached <- struct{}{}
			<-hold
		}
	}
	ref := r.start(r.request(match.ModeAuto))
	<-reached

	_, err := r.M.Estimate(context.Background(), match.Request{ContinueBatchID: ref.BatchID})
	var conflict *gterr.ConflictError
	require.ErrorAs(t, err, &conflict, "a match that has not ended cannot be continued")
	require.NoError(t, r.M.Instruct(context.Background(), ref.BatchID, "表情をやわらかく"))
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, r.M.Instruct(context.Background(), ref.BatchID, "  "), &invalid)
	close(hold)

	v := r.finished(ref.BatchID)
	assert.Equal(t, match.StopReached, v.Stop)
	assert.Empty(t, v.Rounds[0].Instruction)
	assert.Equal(t, "表情をやわらかく", v.Rounds[1].Instruction, "the next fix took the words in")
	assert.Equal(t, 1, v.Rounds[1].BasedOn, "words are about the round the user is looking at: the last one")
	assert.ErrorAs(t, r.M.Instruct(context.Background(), ref.BatchID, "あとから"), &conflict, "a match that is over takes no more words")
	assert.ErrorIs(t, r.M.Instruct(context.Background(), "nope", "x"), gterr.ErrNotFound)
}
