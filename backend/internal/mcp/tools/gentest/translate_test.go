package gentest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

func foxLLM() *gentesttest.FakeLLM {
	return &gentesttest.FakeLLM{Say: func(s string) string { return map[string]string{"赤い狐": "a red fox", "ぼやけ": "blurry"}[s] }}
}

func japanese(extra map[string]any) map[string]any {
	args := map[string]any{
		"media": "image", "models": []string{"nano-banana"},
		"prompts": []map[string]any{{"prompt": "赤い狐", "negative_prompt": "ぼやけ"}},
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// #913: the default of the argument is the screen's (on).
func TestTranslateToEnglishDefaultsToTheScreensDefault(t *testing.T) {
	assert.True(t, gentest.DefaultTranslate)
	llmFake := foxLLM()
	r := newRig(t, gentesttest.WithTranslator(llmFake))

	var omitted, on, off gentest.Estimate
	r.CallOK("estimate_generation_test", japanese(nil), &omitted)
	r.CallOK("estimate_generation_test", japanese(map[string]any{"translate_to_english": true}), &on)
	r.CallOK("estimate_generation_test", japanese(map[string]any{"translate_to_english": false}), &off)

	assert.True(t, omitted.Request.TranslateToEnglish, "omitted is on")
	assert.Equal(t, on.ID, omitted.ID, "omitted and true are the same request")
	assert.False(t, off.Request.TranslateToEnglish)
	assert.NotEqual(t, on.ID, off.ID)
	assert.Empty(t, llmFake.Asked(), "an estimate calls no LLM")
}

func TestEstimateIncludesTheTranslationAsAnUnpricedLine(t *testing.T) {
	r := newRig(t, gentesttest.WithTranslator(foxLLM()))
	var on, off gentest.Estimate
	r.CallOK("estimate_generation_test", japanese(nil), &on)
	r.CallOK("estimate_generation_test", japanese(map[string]any{"translate_to_english": false}), &off)

	require.Len(t, on.Breakdown, 2)
	tl := on.Breakdown[1]
	assert.Equal(t, gentest.TranslateLine, tl.Model)
	assert.Equal(t, 2, tl.Items, "the prompt and the negative prompt")
	assert.Equal(t, "unknown", tl.Basis)
	assert.Contains(t, tl.Label, "anthropic / fake-llm", "the LLM of the assignment")
	assert.Contains(t, on.UnknownModels, gentest.TranslateLine)
	assert.InDelta(t, off.TotalUSD, on.TotalUSD, 1e-9, "the LLM has no unit price: it is left out of the total")
	assert.Equal(t, off.TotalItems, on.TotalItems)
	require.Len(t, off.Breakdown, 1)
}

func TestRunTranslatesAndTheResultKeepsBothTexts(t *testing.T) {
	llmFake := foxLLM()
	r := newRig(t, gentesttest.WithTranslator(llmFake))
	out, _ := r.run(japanese(map[string]any{"count": 2}))
	require.Equal(t, mcpkit.JobSucceeded, r.job(out.JobID).State)

	require.Len(t, llmFake.Asked(), 1, "once per run")
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 2)
	for _, x := range b.Results {
		assert.Equal(t, "赤い狐", x.Prompt)
		assert.True(t, x.Translated)
		assert.Equal(t, "a red fox", x.TranslatedPrompt)
		assert.Equal(t, "blurry", x.TranslatedNegativePrompt)
		assert.Contains(t, x.SentPrompt, "a red fox")
		assert.NotContains(t, x.SentPrompt, "赤い狐")
	}
	var got struct {
		gentest.ResultView
	}
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
	assert.Equal(t, "a red fox", got.TranslatedPrompt)
	for _, c := range r.GT.Fake.Calls() {
		assert.NotContains(t, c.Image.Prompt, "赤い狐", "the model got English")
	}
}

func TestRunWithTranslationOffSendsTheTextAsTyped(t *testing.T) {
	llmFake := foxLLM()
	r := newRig(t, gentesttest.WithTranslator(llmFake))
	out, _ := r.run(japanese(map[string]any{"translate_to_english": false}))
	require.Equal(t, mcpkit.JobSucceeded, r.job(out.JobID).State)
	assert.Empty(t, llmFake.Asked())
	assert.Contains(t, r.GT.Fake.Calls()[0].Image.Prompt, "赤い狐")
}

func TestRunFailsWithUpstreamWhenTheTranslationFails(t *testing.T) {
	r := newRig(t, gentesttest.WithTranslator(&gentesttest.FakeLLM{Err: llm.ErrCircuitOpen}))
	args := japanese(nil)
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	args["estimate_id"], args["confirmed_total_usd"] = est.ID, est.TotalUSD

	msg := r.CallError("run_generation_test", args)
	assert.Contains(t, msg, "upstream")
	assert.Contains(t, msg, "何も生成していません")
	assert.Empty(t, r.GT.Fake.Calls(), "no model was called")
}

func TestEstimateRefusesTranslationWithoutALLMOnlyWhenItIsNeeded(t *testing.T) {
	r := newRig(t)
	assert.Contains(t, r.CallError("estimate_generation_test", japanese(nil)), "validation")
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", japanese(map[string]any{"translate_to_english": false}), &est)
	r.CallOK("estimate_generation_test", batch("nano-banana"), &est) // English: nothing to translate
}
