package settingstest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

func weighted(prompt string, models ...string) map[string]any {
	return map[string]any{"media": "image", "models": models, "prompts": []map[string]any{{"prompt": prompt}}}
}

// #972: a Japanese prompt with weights is translated with the instruction that keeps the weight syntax, and the weights the English kept
// are in what the model is sent.
func TestAPromptWithWeightsIsTranslatedUnderTheInstructionThatKeepsThem(t *testing.T) {
	f := &gentesttest.FakeLLM{Say: func(s string) string {
		return strings.NewReplacer("赤い狐", "red fox", "夕暮れ", "sunset").Replace(s)
	}}
	r := newRig(t, gentesttest.WithTranslator(f))
	out, _ := r.run(weighted("(赤い狐:1.3)、夕暮れ", "nano-banana"))
	require.Equal(t, mcpkit.JobSucceeded, r.job(out.JobID).State)

	asked := f.Asked()
	require.Len(t, asked, 1)
	assert.Contains(t, asked[0].System, "(token:weight)")
	assert.Contains(t, asked[0].System, "keep every parenthesis, bracket, colon and number exactly as written")
	assert.Equal(t, []string{"(赤い狐:1.3)、夕暮れ"}, asked[0].Texts, "the LLM gets the text as it was typed")
	assert.Equal(t, "(red fox:1.3)、sunset", r.GT.Fake.Calls()[0].Image.Prompt)

	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)
	assert.Empty(t, b.Results[0].LostWeights, "nothing was lost")
}

// A translation that drops a weight is found by comparing the numbers and the brackets: the result says which weights are gone, and
// the picture is made anyway (the prompt is the user's, the loss is a notice).
func TestAWeightTheTranslationLostIsSaidOnTheResult(t *testing.T) {
	f := &gentesttest.FakeLLM{Say: func(string) string { return "red fox, (sunset)" }}
	r := newRig(t, gentesttest.WithTranslator(f))
	out, _ := r.run(weighted("(赤い狐:1.3)、(夕暮れ:0.8)", "nano-banana"))
	require.Equal(t, mcpkit.JobSucceeded, r.job(out.JobID).State)

	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)
	assert.Equal(t, []string{"重み :1.3", "重み :0.8", "丸括弧 ( )"}, b.Results[0].LostWeights, "both numbers, and one pair of brackets of two")
	var got record.ResultView
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &got)
	assert.Equal(t, b.Results[0].LostWeights, got.LostWeights)
}

// English prompts with weights are not translated: they reach the model exactly as typed (the weights of a CLIP model keep working).
func TestEnglishWeightsReachTheModelAsTyped(t *testing.T) {
	f := &gentesttest.FakeLLM{}
	r := newRig(t, gentesttest.WithTranslator(f), gentesttest.WithTemplateModels())
	const prompt = "(red fox:1.3), [blurry], ((sunset)), a fox"
	out, _ := r.run(weighted(prompt, "nano-banana"))
	require.Equal(t, mcpkit.JobSucceeded, r.job(out.JobID).State)
	assert.Empty(t, f.Asked(), "English: no call of the LLM")
	assert.Equal(t, prompt, r.GT.Fake.Calls()[0].Image.Prompt)
}

// The estimate says, for each model, whether it reads the weights of the prompt: a model that does not (Qwen-Image's encoder) or
// reads them poorly warns when the prompt has weights, a model that reads them does not, and a prompt without weights warns no one.
func TestTheEstimateWarnsOfModelsThatDoNotReadPromptWeights(t *testing.T) {
	r := newRig(t, gentesttest.WithTemplateModels())
	models := []string{string(gentesttest.SamplingModel), string(gentesttest.WeakWeightModel), string(gentesttest.NoWeightModel)}
	warned := func(prompt string) map[string]string {
		var est gentest.Estimate
		r.CallOK("estimate_generation_test", weighted(prompt, models...), &est)
		out := map[string]string{}
		for _, w := range est.Warnings {
			if w.Code == gentest.WarnPromptWeight {
				out[w.Model] = w.Message
			}
		}
		return out
	}
	got := warned("(red fox:1.3), a barn")
	require.Len(t, got, 2)
	assert.Contains(t, got[string(gentesttest.NoWeightModel)], "ウェイト")
	assert.Contains(t, got[string(gentesttest.NoWeightModel)], "文として読まれる")
	assert.Contains(t, got[string(gentesttest.WeakWeightModel)], "弱いかもしれません")
	assert.NotContains(t, got, string(gentesttest.SamplingModel), "SDXL reads them")
	assert.Empty(t, warned("a red fox in a barn"))
	assert.Empty(t, warned("a fox \\(not a weight\\)"))
}

func TestTheModelListSaysHowEachModelReadsWeights(t *testing.T) {
	r := newRig(t, gentesttest.WithTemplateModels())
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "image"}, &out)
	by := map[string]promptweight.Level{}
	for _, m := range out.Models {
		by[m.ID] = m.PromptWeight
		assert.True(t, m.PromptWeight.Valid(), m.ID)
	}
	assert.Equal(t, promptweight.Supported, by[string(gentesttest.SamplingModel)])
	assert.Equal(t, promptweight.Weak, by[string(gentesttest.WeakWeightModel)])
	assert.Equal(t, promptweight.Unsupported, by[string(gentesttest.NoWeightModel)])
	assert.Equal(t, promptweight.Unknown, by["nano-banana"])
}
