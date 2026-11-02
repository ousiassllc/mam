package match_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

func TestEstimatePricesTheMostRoundsAndLeavesTheLLMUnpriced(t *testing.T) {
	r := newRig(t, nil)
	e, err := r.M.Estimate(context.Background(), r.request(match.ModeAuto))
	require.NoError(t, err)
	assert.Equal(t, 5, e.Rounds)
	assert.InDelta(t, 5*0.03, e.TotalUSD, 1e-9, "the most rounds times the price of one picture")
	require.Len(t, e.Breakdown, 2)
	assert.Equal(t, "flux-2", e.Breakdown[0].Model)
	assert.Equal(t, "priced", e.Breakdown[0].Basis)
	assert.Equal(t, match.LLMLine, e.Breakdown[1].Model)
	assert.Equal(t, "unknown", e.Breakdown[1].Basis, "the LLM has no unit price: it is not in the total")
	var codes []string
	for _, w := range e.Warnings {
		codes = append(codes, w.Model+":"+w.Code)
	}
	assert.Equal(t, []string{match.LLMLine + ":" + gentest.WarnNoPrice}, codes)
	assert.NotEmpty(t, e.ID)
	assert.Empty(t, e.OverLimit)

	for _, mode := range []string{match.ModeOnce, match.ModeAdjust} {
		e, err = r.M.Estimate(context.Background(), r.request(mode))
		require.NoError(t, err)
		assert.Equal(t, 1, e.Rounds, mode)
		assert.InDelta(t, 0.03, e.TotalUSD, 1e-9, mode)
	}
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
