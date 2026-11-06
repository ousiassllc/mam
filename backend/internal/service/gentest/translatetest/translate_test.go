package translatetest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

func english() *gentesttest.FakeLLM {
	words := map[string]string{"夕暮れの海辺の狐": "a fox on the seaside at dusk", "ぼやけ": "blurry"}
	return &gentesttest.FakeLLM{Say: func(s string) string { return words[s] }}
}

func jaReq(translate bool, models ...string) gentest.Request {
	return gentest.Request{
		Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "夕暮れの海辺の狐", Negative: "ぼやけ"}}, TranslateToEnglish: translate,
	}
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

// #1069: the call of the Reasoning LLM is priced per token at the price of the target of its route (llm.PriceOf), so the translation
// is in the total, and the confirmation is for the figure that includes it.
func TestEstimateWithTranslationIsInTheTotalAtThePriceOfTheRoutesTarget(t *testing.T) {
	haiku := llm.Target{Provider: llm.ProviderAnthropic, Model: "claude-haiku-4-5"}
	llmFake := english()
	r := gentesttest.New(t, gentesttest.WithTranslatorAt(llmFake, haiku))

	off, err := r.Svc.Estimate(context.Background(), jaReq(false, "flux-2"))
	require.NoError(t, err)
	on, err := r.Svc.Estimate(context.Background(), jaReq(true, "flux-2"))
	require.NoError(t, err)

	tl := line(t, on, gentest.TranslateLine)
	assert.Equal(t, 2, tl.Items)
	assert.Equal(t, "priced", tl.Basis)
	require.NotNil(t, tl.SubtotalUSD)
	require.NotNil(t, tl.UnitUSD)
	assert.Positive(t, *tl.SubtotalUSD)
	assert.InDelta(t, *tl.SubtotalUSD, *tl.UnitUSD*float64(tl.Items), 1e-4, "the unit is the share of one text in the call")
	assert.Contains(t, tl.Label, "claude-haiku-4-5", "the LLM of the assignment is named")
	assert.InDelta(t, off.TotalUSD+*tl.SubtotalUSD, on.TotalUSD, 1e-4, "the translation is in the total")
	assert.NotContains(t, on.UnknownModels, gentest.TranslateLine)
	assert.Equal(t, off.TotalItems, on.TotalItems, "the line is not a result")
	assert.Equal(t, off.UnknownItems, on.UnknownItems)
	for _, w := range on.Warnings {
		assert.False(t, w.Model == gentest.TranslateLine, "no warning of a missing price: %+v", w)
	}
	assert.Empty(t, llmFake.Asked(), "an estimate calls no LLM")

	// the confirmation is for the total with the translation: Run takes it.
	ref, err := r.Confirmed(jaReq(true, "flux-2"))
	require.NoError(t, err)
	assert.Equal(t, gentest.StateSucceeded, r.Wait(ref.BatchID).State)
}

func TestEstimateWithTranslationThroughALocalModelIsFree(t *testing.T) {
	local := llm.Target{Provider: llm.ProviderLocal, Model: "qwen"}
	r := gentesttest.New(t, gentesttest.WithTranslatorAt(english(), local))
	off, err := r.Svc.Estimate(context.Background(), jaReq(false, "flux-2"))
	require.NoError(t, err)
	on, err := r.Svc.Estimate(context.Background(), jaReq(true, "flux-2"))
	require.NoError(t, err)

	tl := line(t, on, gentest.TranslateLine)
	assert.Equal(t, "free", tl.Basis)
	assert.InDelta(t, off.TotalUSD, on.TotalUSD, 1e-9)
	assert.NotContains(t, on.UnknownModels, gentest.TranslateLine)
}

// #1072: the request that is cancelled while the LLM is asked is cancelled, not refused as "the LLM failed" (502 upstream).
func TestRunCancelledWhileTranslatingIsNotAnUpstreamFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	llmFake := &gentesttest.FakeLLM{Err: context.Canceled, OnCall: cancel}
	r := gentesttest.New(t, gentesttest.WithTranslator(llmFake))
	req := jaReq(true, "flux-2")
	est, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)

	_, err = r.Svc.Run(ctx, req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.ErrorIs(t, err, context.Canceled)
	var up *gterr.UpstreamError
	assert.False(t, errors.As(err, &up), "a cancelled request is no failure of the LLM: %v", err)
	assert.Empty(t, r.Fake.Calls())
	batches, err := r.Svc.Batches(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, batches, "no batch was made")
}
