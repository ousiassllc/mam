package match_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

func warned(e match.Estimate) []string {
	var codes []string
	for _, w := range e.Warnings {
		codes = append(codes, w.Model+":"+w.Code)
	}
	return codes
}

func TestEstimatePricesTheMostRoundsAndWarnsOfTheLLMLinesWithoutAPrice(t *testing.T) {
	r := newRig(t, nil) // the targets of the fake LLM are not on any price page
	e, err := r.M.Estimate(context.Background(), r.request(match.ModeAuto))
	require.NoError(t, err)
	assert.Equal(t, 5, e.Rounds)
	assert.InDelta(t, 5*0.03, e.TotalUSD, 1e-9, "the most rounds times the price of one picture")
	require.Len(t, e.Breakdown, 3)
	assert.Equal(t, "flux-2", e.Breakdown[0].Model)
	assert.Equal(t, "priced", e.Breakdown[0].Basis)
	for i, model := range []string{match.LLMLine, match.CompareLine} {
		line := e.Breakdown[i+1]
		assert.Equal(t, model, line.Model)
		assert.Equal(t, "unknown", line.Basis, "no checked price: the line is not in the total")
		assert.Nil(t, line.UnitUSD)
		assert.Nil(t, line.SubtotalUSD)
		assert.Equal(t, 5, line.Items)
	}
	assert.Equal(t, []string{match.LLMLine + ":" + gentest.WarnNoPrice, match.CompareLine + ":" + gentest.WarnNoPrice}, warned(e))
	assert.NotEmpty(t, e.ID)
	assert.Empty(t, e.OverLimit)

	for _, mode := range []string{match.ModeOnce, match.ModeAdjust} {
		e, err = r.M.Estimate(context.Background(), r.request(mode))
		require.NoError(t, err)
		assert.Equal(t, 1, e.Rounds, mode)
		assert.InDelta(t, 0.03, e.TotalUSD, 1e-9, mode)
	}
}

var (
	opus   = llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"}
	gptMin = llm.Target{Provider: llm.ProviderOpenAI, Model: "gpt-5-mini"}
)

func TestEstimateAddsThePerTokenPriceOfTheLLMCallsToTheTotalWithoutANoPriceWarning(t *testing.T) {
	r := newPricedRig(t, opus, gptMin)
	e, err := r.M.Estimate(context.Background(), r.request(match.ModeAuto))
	require.NoError(t, err)
	require.Len(t, e.Breakdown, 3)
	write, compare := e.Breakdown[1], e.Breakdown[2]
	for _, line := range []gentest.ModelEstimate{write, compare} {
		assert.Equal(t, "priced", line.Basis)
		require.NotNil(t, line.UnitUSD)
		require.NotNil(t, line.SubtotalUSD)
		assert.Equal(t, 5, line.Items, "one write and one comparison a round")
		assert.InDelta(t, *line.UnitUSD*5, *line.SubtotalUSD, 1e-4)
	}
	assert.Contains(t, write.Label, "claude-opus-4-5")
	assert.Contains(t, compare.Label, "gpt-5-mini")
	assert.InDelta(t, (5700*5+2500*25)/1e6, *write.UnitUSD, 1e-9, "Opus 4.5: $5 in, $25 out per million tokens")
	assert.InDelta(t, (3700*0.25+1000*2)/1e6, *compare.UnitUSD, 1e-9, "GPT-5 mini: $0.25 in, $2 out per million tokens")
	pictures := 5 * 0.03
	assert.InDelta(t, pictures+*write.SubtotalUSD+*compare.SubtotalUSD, e.TotalUSD, 1e-4, "the LLM is in the total")
	assert.Greater(t, e.TotalUSD, pictures)
	assert.Empty(t, e.Warnings, "nothing is left out of the total, so nothing is warned of")
}

func TestEstimateWarnsOnlyOfTheLLMLineWhosePriceIsNotKnown(t *testing.T) {
	plan := llm.Target{Provider: llm.ProviderChatGPTSubscription, Model: "gpt-5"}
	r := newPricedRig(t, plan, gptMin)
	e, err := r.M.Estimate(context.Background(), r.request(match.ModeOnce))
	require.NoError(t, err)
	assert.Equal(t, []string{match.LLMLine + ":" + gentest.WarnNoPrice}, warned(e), "the plan is not per call: its line stays out of the total")
	assert.Equal(t, "unknown", e.Breakdown[1].Basis)
	assert.Equal(t, "priced", e.Breakdown[2].Basis)
	assert.InDelta(t, 0.03+*e.Breakdown[2].SubtotalUSD, e.TotalUSD, 1e-4)
}

func TestTheLLMCostCountsAgainstTheCostTheUserAllowed(t *testing.T) {
	r := newPricedRig(t, opus, opus, gentesttest.WithNoReferenceModel()) // the pictures are free: only the LLM costs
	req := r.request(match.ModeAuto)
	req.Model = string(gentesttest.NoReferenceModel)
	e, err := r.M.Estimate(context.Background(), req)
	require.NoError(t, err)
	require.Greater(t, e.TotalUSD, 0.0, "the LLM calls are the whole cost")
	req.MaxCostUSD = e.TotalUSD / 2
	e, err = r.M.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, e.OverLimit, "指定した上限の費用")
	_, err = r.M.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	var c *gterr.ConflictError
	assert.ErrorAs(t, err, &c, "the match does not start when the LLM alone is over the cost")
	assert.Zero(t, r.LLM.Count(gentesttest.CallDescribe))
}

func TestEstimateOfALocalModelIsFree(t *testing.T) {
	r := newRig(t, nil, gentesttest.WithNoReferenceModel())
	req := r.request(match.ModeAuto)
	req.Model = string(gentesttest.NoReferenceModel)
	e, err := r.M.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "free", e.Breakdown[0].Basis)
	assert.Zero(t, e.TotalUSD)
}

func TestEstimateIsOverTheLimitWhenTheRoundsCostMoreThanTheCap(t *testing.T) {
	r := newRig(t, nil, gentesttest.WithConfig(func(c *modeladapter.Config) { cost := 0.10; c.Gentest.MaxCostUSD = &cost }))
	req := r.request(match.ModeAuto)
	e, err := r.M.Estimate(context.Background(), req) // 5 x 0.03 = 0.15 > 0.10
	require.NoError(t, err)
	assert.NotEmpty(t, e.OverLimit)
	_, err = r.M.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	var c *gterr.ConflictError
	require.ErrorAs(t, err, &c, "a match that costs more than the limit does not start")
	assert.Zero(t, r.LLM.Count(gentesttest.CallDescribe), "nothing was asked of the LLM")
	assert.Empty(t, r.Fake.Calls(), "and nothing was generated")

	req.MaxRounds = 3 // 0.09
	e, err = r.M.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, e.OverLimit)
}

func TestEstimateIsOverTheCostTheUserAllowedEvenWithinTheCapOfTheSetting(t *testing.T) {
	r := newRig(t, nil)
	req := r.request(match.ModeAuto)
	req.MaxCostUSD = 0.10 // 5 x 0.03 = 0.15
	e, err := r.M.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Contains(t, e.OverLimit, "指定した上限の費用")
	_, err = r.M.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	var c *gterr.ConflictError
	require.ErrorAs(t, err, &c, "a match that costs more than the user allowed does not start")
	assert.Empty(t, r.Fake.Calls())
	assert.Zero(t, r.LLM.Count(gentesttest.CallDescribe))

	req.MaxRounds = 3 // 0.09 fits
	e, err = r.M.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, e.OverLimit)

	req.MaxCostUSD = -1
	_, err = r.M.Estimate(context.Background(), req)
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
}

func TestEstimateRefusesWhatCannotRun(t *testing.T) {
	r := newRig(t, nil, gentesttest.WithoutBackend(domain.ModelNanoBanana))
	good := r.request(match.ModeAuto)
	for name, mod := range map[string]func(*match.Request){
		"no reference":        func(q *match.Request) { q.Reference = gentest.RefInput{} },
		"gone reference":      func(q *match.Request) { q.Reference.ID = "00000000-gone.png" },
		"no model":            func(q *match.Request) { q.Model = "" },
		"unknown model":       func(q *match.Request) { q.Model = "nope" },
		"video model":         func(q *match.Request) { q.Model = "kling-3.0" },
		"model without a key": func(q *match.Request) { q.Model = "nano-banana" },
		"unknown mode":        func(q *match.Request) { q.Mode = "sometimes" },
		"target over 100":     func(q *match.Request) { q.TargetScore = 101 },
		"too many rounds":     func(q *match.Request) { q.MaxRounds = match.MaxRounds + 1 },
		"bad aspect":          func(q *match.Request) { q.AspectRatio = "wide" },
		"long words":          func(q *match.Request) { q.Instruction = string(make([]rune, match.MaxInstructionRunes+1)) },
		"continue unknown":    func(q *match.Request) { *q = match.Request{ContinueBatchID: "nope"} },
	} {
		req := good
		mod(&req)
		_, err := r.M.Estimate(context.Background(), req)
		assert.Error(t, err, name)
		var invalid *gterr.InvalidError
		var notFound = errors.Is(err, gterr.ErrNotFound)
		assert.True(t, errors.As(err, &invalid) || notFound, "%s: %v", name, err)
	}
}

func TestEstimateRefusesWhenThereIsNoLLM(t *testing.T) {
	r := gentesttest.New(t)
	m := match.New(match.Deps{Gentest: r.Svc})
	id, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	_, err = m.Estimate(context.Background(), match.Request{Reference: gentest.RefInput{Source: gentest.FromStaged, ID: id}, Model: "flux-2"})
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)
}

func TestRunIsRefusedWithoutTheConfirmedEstimate(t *testing.T) {
	r := newRig(t, nil)
	req := r.request(match.ModeAuto)
	e, err := r.M.Estimate(context.Background(), req)
	require.NoError(t, err)

	_, err = r.M.Run(context.Background(), req, gentest.Confirmation{})
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid, "no estimate_id")
	_, err = r.M.Run(context.Background(), req, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD + 1})
	var conflict *gterr.ConflictError
	assert.ErrorAs(t, err, &conflict, "another total")
	changed := req
	changed.TargetScore = 90
	_, err = r.M.Run(context.Background(), changed, gentest.Confirmation{EstimateID: e.ID, TotalUSD: e.TotalUSD})
	assert.ErrorAs(t, err, &conflict, "another request")
	assert.Empty(t, r.Fake.Calls())
}
