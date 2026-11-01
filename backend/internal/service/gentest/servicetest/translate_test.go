package servicetest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// english is a Reasoning LLM that translates the Japanese of these tests.
func english() *gentesttest.FakeLLM {
	words := map[string]string{"夕暮れの海辺の狐": "a fox on the seaside at dusk", "ぼやけ": "blurry", "夕暮れの波": "waves at dusk", "文字": "text"}
	return &gentesttest.FakeLLM{Say: func(s string) string { return words[s] }}
}

func jaReq(translate bool, models ...string) gentest.Request {
	return gentest.Request{
		Media: "image", Models: models, Prompts: []gentest.PromptInput{{Prompt: "夕暮れの海辺の狐", Negative: "ぼやけ"}}, TranslateToEnglish: translate,
	}
}

func TestDefaultTranslateIsOn(t *testing.T) {
	assert.True(t, gentest.DefaultTranslate, "the screen and the MCP tools start with the translation on")
}

func TestEstimateWithTranslationHasAnUnpricedLineAndCallsNoLLM(t *testing.T) {
	llmFake := english()
	r := gentesttest.New(t, gentesttest.WithTranslator(llmFake))

	off, err := r.Svc.Estimate(context.Background(), jaReq(false, "flux-2"))
	require.NoError(t, err)
	on, err := r.Svc.Estimate(context.Background(), jaReq(true, "flux-2"))
	require.NoError(t, err)

	tl := line(t, on, gentest.TranslateLine)
	assert.Equal(t, 2, tl.Items, "the prompt and the negative prompt")
	assert.Equal(t, "unknown", tl.Basis, "an LLM call has no unit price: left out of the total, as a model without a price is")
	assert.Nil(t, tl.UnitUSD)
	assert.Nil(t, tl.SubtotalUSD)
	assert.Contains(t, tl.Label, "anthropic / fake-llm", "the LLM of the assignment is named")
	assert.InDelta(t, off.TotalUSD, on.TotalUSD, 1e-9)
	assert.Equal(t, off.TotalItems, on.TotalItems, "the line is not a result")
	assert.Contains(t, on.UnknownModels, gentest.TranslateLine)
	assert.Equal(t, 0, on.UnknownItems, "UnknownItems counts results")
	var warned bool
	for _, w := range on.Warnings {
		warned = warned || (w.Code == gentest.WarnNoPrice && w.Model == gentest.TranslateLine)
	}
	assert.True(t, warned, "%+v", on.Warnings)
	assert.True(t, on.Request.TranslateToEnglish)
	assert.NotEqual(t, off.ID, on.ID, "the switch is part of what is confirmed")
	assert.Empty(t, llmFake.Asked(), "an estimate calls no LLM")
	for _, l := range off.Breakdown {
		assert.NotEqual(t, gentest.TranslateLine, l.Model)
	}
}

func TestEstimateHasNoTranslationLineForEnglishPrompts(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithTranslator(english()))
	req := imageReq("flux-2")
	req.TranslateToEnglish = true
	e, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	assert.Len(t, e.Breakdown, 1)
	assert.Empty(t, e.UnknownModels)
}

func TestTranslationIsRefusedWithoutAnLLMOnlyWhenSomethingNeedsIt(t *testing.T) {
	r := gentesttest.New(t)
	_, err := r.Svc.Estimate(context.Background(), jaReq(true, "flux-2"))
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "英訳に使う LLM")

	english := imageReq("flux-2")
	english.TranslateToEnglish = true
	_, err = r.Svc.Estimate(context.Background(), english)
	assert.NoError(t, err, "an English prompt needs no LLM")
	_, err = r.Svc.Estimate(context.Background(), jaReq(false, "flux-2"))
	assert.NoError(t, err, "switched off")
}

func TestRunTranslatesOnceForEveryModelAndEveryCallAndKeepsBothInTheHistory(t *testing.T) {
	llmFake := english()
	r := gentesttest.New(t, gentesttest.WithTranslator(llmFake))
	req := jaReq(true, "flux-2", "nano-banana")
	req.Count = 2

	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)

	asked := llmFake.Asked()
	require.Len(t, asked, 1, "once per run, not per model or per call")
	assert.Equal(t, llm.AgentPromptPlanner, asked[0].Agent, "the Prompt Planner's assignment")
	assert.Equal(t, []string{"夕暮れの海辺の狐", "ぼやけ"}, asked[0].Texts)
	require.Len(t, st.Results, 4)
	for _, res := range st.Results {
		assert.Equal(t, "夕暮れの海辺の狐", res.Prompt, "the prompt as typed")
		assert.Equal(t, "ぼやけ", res.NegativePrompt)
		assert.True(t, res.Translated)
		assert.Equal(t, "a fox on the seaside at dusk", res.TranslatedPrompt)
		assert.Equal(t, "blurry", res.TranslatedNegativePrompt)
	}
	for _, m := range []domain.ModelName{domain.ModelFlux2, domain.ModelNanoBanana} {
		for _, c := range r.Fake.CallsOf(m) {
			assert.Contains(t, c.Image.Prompt, "a fox on the seaside at dusk", string(m)+": the English is what the model gets")
			assert.NotContains(t, c.Image.Prompt, "夕暮れ", string(m)+": not the Japanese")
		}
	}
	assert.Len(t, r.Fake.Calls(), 4)
	fromHistory, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	assert.Equal(t, st.Results[0].TranslatedPrompt, fromHistory.TranslatedPrompt, "kept in the history, not only in the batch")
	assert.Equal(t, "夕暮れの海辺の狐", fromHistory.Prompt)
	prompts, err := r.Svc.Prompts(context.Background())
	require.NoError(t, err)
	require.Len(t, prompts, 1)
	assert.Equal(t, "夕暮れの海辺の狐", prompts[0].Prompt, "the prompt history keeps what was typed")
}

func TestRunSendsTheEnglishNegativeInTheModelsFieldOrFoldedIntoThePrompt(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithTranslator(english()))
	ref, err := r.Confirmed(gentest.Request{
		Media: "video", Models: []string{"kling-3.0", "seedance-2.0"}, Resolution: "720p", DurationSeconds: 5, TranslateToEnglish: true,
		Prompts: []gentest.PromptInput{{Prompt: "夕暮れの波", Negative: "文字"}},
	})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)

	kling := r.Fake.CallsOf(domain.ModelKling3)
	require.Len(t, kling, 1)
	assert.Equal(t, "waves at dusk", kling[0].Video.Prompt)
	assert.Equal(t, "text", kling[0].Video.NegativePrompt, "the field gets the English")
	seedance := r.Fake.CallsOf(domain.ModelSeedance2)
	require.Len(t, seedance, 1)
	assert.Equal(t, direct.Fold("waves at dusk", "text"), seedance[0].Video.Prompt, "folded from the English")
	assert.Contains(t, seedance[0].Video.Prompt, "text")
	assert.NotContains(t, seedance[0].Video.Prompt, "文字")
	for _, res := range st.Results {
		assert.Equal(t, "waves at dusk", res.TranslatedPrompt)
		assert.Equal(t, "text", res.TranslatedNegativePrompt)
	}
}

func TestRunLeavesEnglishAndSendsOnlyWhatIsNotEnglishToTheLLM(t *testing.T) {
	llmFake := english()
	r := gentesttest.New(t, gentesttest.WithTranslator(llmFake))
	ref, err := r.Confirmed(gentest.Request{
		Media: "image", Models: []string{"flux-2"}, TranslateToEnglish: true,
		Prompts: []gentest.PromptInput{{Prompt: "a fox", Negative: "ぼやけ"}, {Prompt: "a cat"}},
	})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)

	require.Len(t, llmFake.Asked(), 1)
	assert.Equal(t, []string{"ぼやけ"}, llmFake.Asked()[0].Texts, "English text is never sent to the LLM")
	byPrompt := map[string]gentest.ResultView{}
	for _, res := range st.Results {
		byPrompt[res.Prompt] = res
	}
	assert.True(t, byPrompt["a fox"].Translated)
	assert.Equal(t, "a fox", byPrompt["a fox"].TranslatedPrompt)
	assert.Equal(t, "blurry", byPrompt["a fox"].TranslatedNegativePrompt)
	assert.False(t, byPrompt["a cat"].Translated, "English already: sent as it is, nothing recorded")
	assert.Equal(t, "a cat", byPrompt["a cat"].SentPrompt)
}

func TestRunWithEnglishOnlyMakesNoLLMCall(t *testing.T) {
	llmFake := &gentesttest.FakeLLM{Err: errors.New("must not be called")}
	r := gentesttest.New(t, gentesttest.WithTranslator(llmFake))
	req := imageReq("flux-2")
	req.TranslateToEnglish = true
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	r.Wait(ref.BatchID)
	assert.Empty(t, llmFake.Asked())
	assert.Equal(t, "a fox", r.Fake.Calls()[0].Image.Prompt)
}

func TestRunWithTheSwitchOffSendsThePromptAsTypedAndCallsNoLLM(t *testing.T) {
	llmFake := english()
	r := gentesttest.New(t, gentesttest.WithTranslator(llmFake))
	ref, err := r.Confirmed(jaReq(false, "flux-2"))
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)

	assert.Empty(t, llmFake.Asked())
	assert.Contains(t, r.Fake.Calls()[0].Image.Prompt, "夕暮れの海辺の狐")
	assert.False(t, st.Results[0].Translated)
	assert.Empty(t, st.Results[0].TranslatedPrompt)
}

func TestRunStopsWithAnErrorWhenTheTranslationFailsAndSendsNothing(t *testing.T) {
	for name, fake := range map[string]*gentesttest.FakeLLM{
		"the LLM fails":      {Err: llm.ErrNoAPIKey},
		"circuit open":       {Err: llm.ErrCircuitOpen},
		"timeout":            {Err: llm.ErrTimeout},
		"returns the same":   {Say: func(s string) string { return s }},
		"returns an empty":   {Say: func(string) string { return "  " }},
		"not the asked JSON": {Err: llm.ErrInvalidOutput},
	} {
		r := gentesttest.New(t, gentesttest.WithTranslator(fake))
		req := jaReq(true, "flux-2", "nano-banana")
		est, err := r.Svc.Estimate(context.Background(), req)
		require.NoError(t, err, name)

		_, err = r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
		var up *gterr.UpstreamError
		require.ErrorAs(t, err, &up, name)
		assert.Contains(t, up.Message, "何も生成していません", name)
		assert.Contains(t, up.Message, "英訳を OFF", name)
		assert.Empty(t, r.Fake.Calls(), name+": no model was called, in Japanese or otherwise")
		batches, err := r.Svc.Batches(context.Background(), 10)
		require.NoError(t, err)
		assert.Empty(t, batches, name+": no batch was made")
		assert.Equal(t, 0, countRows(t, r, "gentest_result"), name)
		assert.Equal(t, 0, countRows(t, r, "gentest_prompt"), name)
	}
}

func TestTranslationFailureIsTheReasonOfTheCause(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithTranslator(&gentesttest.FakeLLM{Err: llm.ErrNoAPIKey}))
	req := jaReq(true, "flux-2")
	est, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
	_, err = r.Svc.Run(context.Background(), req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	var up *gterr.UpstreamError
	require.ErrorAs(t, err, &up)
	assert.Contains(t, up.Message, "API キー")
	assert.ErrorIs(t, err, llm.ErrNoAPIKey, "the cause is kept for the log")
}
