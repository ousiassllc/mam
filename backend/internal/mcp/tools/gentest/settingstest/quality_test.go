package settingstest_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest/intentinfo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

func installed() gentesttest.Option {
	return gentesttest.WithInstalled(map[string]string{"sdxl-base": "txt2img-sdxl", "qwen-image": "txt2img-qwen-image"})
}

// #977: quality と intent は estimate / run の引数で、モデルの一覧には intent ごとにいま選ばれるモデルが出る。
func TestQualityAndIntentAreInTheSchemaOfEstimateAndRun(t *testing.T) {
	r := newRig(t)
	tools, err := r.Client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	seen := 0
	for _, tool := range tools.Tools {
		if tool.Name != "estimate_generation_test" && tool.Name != "run_generation_test" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)
		var schema struct {
			Required []string `json:"required"`
			Props    map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(raw, &schema))
		assert.Contains(t, schema.Props["quality"].Description, "quality_ignored", tool.Name)
		assert.Contains(t, schema.Props["intent"].Description, "face", tool.Name)
		assert.NotContains(t, schema.Required, "models", tool.Name+": an intent leaves the model to mam")
		seen++
	}
	assert.Equal(t, 2, seen)
}

func TestTheModelListNamesTheModelEachIntentPicksNow(t *testing.T) {
	r := newRig(t, installed())
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "image"}, &out)
	by := map[string]intentinfo.IntentInfo{}
	for _, in := range out.Intents {
		by[in.ID] = in
	}
	require.Len(t, by, 4)
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), by["photo"].Model)
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), by["landscape"].Model)
	assert.Contains(t, by["anime"].Note, "アニメ専用")
	assert.True(t, by["face"].NeedsReferences)
	assert.Empty(t, by["face"].Model, "no installed model reads the face role")
	assert.Contains(t, by["face"].Reason, "導入されていません")

	empty := newRig(t)
	var none mcpgentest.ListModelsOutput
	empty.CallOK("list_generation_models", map[string]any{"media": "image"}, &none)
	for _, in := range none.Intents {
		assert.Empty(t, in.Model, in.ID)
		assert.Contains(t, in.Reason, "モデルマネージャー", in.ID)
	}
}

// quality: fast は、モデルのプリセットのステップ数で作り、結果に残る。省いた引数は今までどおり（モデルの既定）。
func TestAQualityRunsTheModelAtItsPresetAndTheResultKeepsIt(t *testing.T) {
	r := newRig(t, installed())
	model := gentesttest.InstalledID("sdxl-base")
	stepsOf := func(extra map[string]any) int {
		args := map[string]any{"media": "image", "models": []string{model}, "prompts": []map[string]any{{"prompt": "a red fox"}}}
		for k, v := range extra {
			args[k] = v
		}
		out, _ := r.run(args)
		require.Equal(t, "succeeded", string(r.job(out.JobID).State))
		var b gentest.BatchStatus
		r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
		require.Len(t, b.Results, 1)
		require.NotNil(t, b.Results[0].Sampling)
		return *b.Results[0].Sampling.Steps
	}
	assert.Equal(t, 25, stepsOf(nil))
	assert.Equal(t, 25, stepsOf(map[string]any{"quality": "standard"}))
	assert.Less(t, stepsOf(map[string]any{"quality": "fast"}), 25)
	assert.Greater(t, stepsOf(map[string]any{"quality": "high"}), 25)
	assert.Equal(t, 33, stepsOf(map[string]any{"quality": "high", "sampling": map[string]any{"steps": 33}}), "the steps the caller named win")

	assert.Contains(t, r.CallError("estimate_generation_test", map[string]any{"media": "image", "models": []string{model}, "prompts": []map[string]any{{"prompt": "x"}}, "quality": "ultra"}), "validation")
}

// intent: モデルを mam が選ぶ。結果に使ったモデルが残る。models との同時指定・合うモデルが無いときは理由つきの validation。
func TestAnIntentPicksTheModelAndRefusesAnImpossibleAsk(t *testing.T) {
	r := newRig(t, installed())
	args := map[string]any{"media": "image", "intent": "photo", "quality": "fast", "prompts": []map[string]any{{"prompt": "a portrait"}}}
	out, est := r.run(args)
	require.Equal(t, "succeeded", string(r.job(out.JobID).State))
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 1)
	assert.Equal(t, gentesttest.InstalledID("qwen-image"), b.Results[0].Model)
	assert.Equal(t, []string{gentesttest.InstalledID("qwen-image")}, est.Request.Models)

	both := map[string]any{"media": "image", "intent": "photo", "models": []string{"nano-banana"}, "prompts": []map[string]any{{"prompt": "x"}}}
	assert.Contains(t, r.CallError("estimate_generation_test", both), "validation")
	face := map[string]any{"media": "image", "intent": "face", "prompts": []map[string]any{{"prompt": "x"}}}
	assert.Contains(t, r.CallError("estimate_generation_test", face), "validation")
}
