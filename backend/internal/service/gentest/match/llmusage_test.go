package match_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

// The cost of the LLM of a match is recorded from what the providers said the calls used (#981), not from the assumption of the estimate.
func TestTheLLMCostOfAMatchIsMeasuredFromTheTokensTheProviderReported(t *testing.T) {
	answered := llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"} // $5 in, $25 out per million tokens
	r := newPricedRig(t, opus, gptMin)
	r.LLM.Scores = []int{40, 90}
	r.LLM.Answered = answered
	r.LLM.Usage = map[string]llm.Usage{
		gentesttest.CallDescribe: {InputTokens: 1000, OutputTokens: 100},
		gentesttest.CallCompare:  {InputTokens: 3000, OutputTokens: 200},
		gentesttest.CallRevise:   {InputTokens: 4000, OutputTokens: 300},
	}
	v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
	require.Len(t, v.Rounds, 2)

	usd := func(in, out float64) float64 { return (in*5 + out*25) / 1e6 }
	kinds := func(rd match.Round) []string {
		var k []string
		for _, c := range rd.LLM {
			k = append(k, c.Kind)
		}
		return k
	}
	assert.Equal(t, []string{imagematch.CallDescribe, imagematch.CallCompare}, kinds(v.Rounds[0]))
	assert.Equal(t, []string{imagematch.CallRevise, imagematch.CallCompare}, kinds(v.Rounds[1]))
	for _, rd := range v.Rounds {
		for _, c := range rd.LLM {
			assert.True(t, c.Measured, c.Kind)
			assert.Equal(t, answered.Provider, c.Provider)
			assert.Equal(t, answered.Model, c.Model)
			require.NotNil(t, c.USD, c.Kind)
		}
	}
	describe := v.Rounds[0].LLM[0]
	assert.Equal(t, 1000, describe.InputTokens)
	assert.Equal(t, 100, describe.OutputTokens)
	assert.InDelta(t, usd(1000, 100), *describe.USD, 1e-12)
	assert.InDelta(t, usd(3000, 200), *v.Rounds[0].LLM[1].USD, 1e-12)
	assert.InDelta(t, usd(4000, 300), *v.Rounds[1].LLM[0].USD, 1e-12)
	assert.InDelta(t, usd(1000, 100)+2*usd(3000, 200)+usd(4000, 300), v.LLMUSD, 1e-12, "the total of the match is the sum of the measured calls")
	assert.False(t, v.LLMAssumed)

	e, err := r.M.Estimate(t.Context(), r.request(match.ModeAuto))
	require.NoError(t, err)
	assert.Less(t, v.LLMUSD, *e.Breakdown[1].SubtotalUSD+*e.Breakdown[2].SubtotalUSD, "the estimate is on the safe side of what was measured")
}

func TestACallWithoutAReportedUsageIsRecordedAsAssumedAndNotAsZero(t *testing.T) {
	r := newPricedRig(t, opus, gptMin)
	r.LLM.Scores = []int{90}
	r.LLM.Answered = opus // the answers say which model answered but not how many tokens they used
	v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
	require.Len(t, v.Rounds, 1)
	require.Len(t, v.Rounds[0].LLM, 2)
	for _, c := range v.Rounds[0].LLM {
		assert.False(t, c.Measured, "the tokens are the estimate's assumption: the record says so")
		assert.Positive(t, c.InputTokens, "an unknown usage is not zero")
		assert.Positive(t, c.OutputTokens)
		require.NotNil(t, c.USD)
		assert.Positive(t, *c.USD)
	}
	assert.True(t, v.LLMAssumed)
	assert.Positive(t, v.LLMUSD)
}

func TestACallOfAModelWithoutAPriceIsRecordedWithTokensAndNoCost(t *testing.T) {
	r := newRig(t, []int{90}) // the fake answers as anthropic / fake-llm: no price
	r.LLM.Usage = map[string]llm.Usage{gentesttest.CallDescribe: {InputTokens: 10, OutputTokens: 5}, gentesttest.CallCompare: {InputTokens: 20, OutputTokens: 5}}
	v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
	require.Len(t, v.Rounds[0].LLM, 2)
	for _, c := range v.Rounds[0].LLM {
		assert.True(t, c.Measured)
		assert.Nil(t, c.USD)
	}
	assert.Zero(t, v.LLMUSD, "a call without a price is not in the total")
}

// A call that was answered with something the match cannot use (not JSON, a prompt that did not change) was paid for: it is in the
// calls of the round and in the total, with the round that failed because of it (#994).
func TestAPaidCallWhoseAnswerCannotBeUsedStaysInTheCostOfTheMatch(t *testing.T) {
	answered := llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-opus-4-5"}
	one := llm.Usage{InputTokens: 1000, OutputTokens: 100}
	usd := (1000*5 + 100*25) / 1e6
	for name, tc := range map[string]struct {
		kind, text string
		calls      []string // the kinds recorded in the round that failed
		rounds     int
	}{
		"the prompt is not JSON":     {gentesttest.CallDescribe, "no JSON here", []string{imagematch.CallDescribe}, 1},
		"the comparison is not JSON": {gentesttest.CallCompare, "no JSON here", []string{imagematch.CallDescribe, imagematch.CallCompare}, 1},
		"the fix is not JSON":        {gentesttest.CallRevise, "no JSON here", []string{imagematch.CallRevise}, 2},
		"the fix did not change":     {gentesttest.CallRevise, `{"prompt": "prompt 1"}`, []string{imagematch.CallRevise}, 2},
		"the score is out of 0..100": {gentesttest.CallCompare, `{"score": 500}`, []string{imagematch.CallDescribe, imagematch.CallCompare}, 1},
	} {
		r := newPricedRig(t, opus, gptMin)
		r.LLM.Scores = []int{40, 90}
		r.LLM.Answered = answered
		r.LLM.Usage = map[string]llm.Usage{gentesttest.CallDescribe: one, gentesttest.CallCompare: one, gentesttest.CallRevise: one}
		r.LLM.Text = map[string]string{tc.kind: tc.text}
		v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
		require.Equal(t, match.StopError, v.Stop, name)
		require.Len(t, v.Rounds, tc.rounds, name)
		var kinds []string
		for _, c := range v.Rounds[len(v.Rounds)-1].LLM {
			kinds = append(kinds, c.Kind)
		}
		assert.Equal(t, tc.calls, kinds, name)
		var calls int
		for _, rd := range v.Rounds {
			calls += len(rd.LLM)
		}
		assert.InDelta(t, float64(calls)*usd, v.LLMUSD, 1e-12, "every call that was answered is in the total: %s", name)
	}
}
