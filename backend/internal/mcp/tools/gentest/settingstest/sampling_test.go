package settingstest_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

func sdxlBatch(extra map[string]any) map[string]any {
	args := map[string]any{"media": "image", "models": []string{string(gentesttest.SamplingModel)}, "prompts": []map[string]any{{"prompt": "a red fox"}}}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// #973: seed and sampling are arguments of both estimate and run, and the model list says which models take what, with their defaults.
func TestSeedAndSamplingAreInTheSchemaOfEstimateAndRun(t *testing.T) {
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
			Props map[string]struct {
				Description string `json:"description"`
				Properties  map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(raw, &schema))
		assert.Contains(t, schema.Props["seed"].Description, "seed_ignored", tool.Name)
		assert.Contains(t, schema.Props["sampling"].Description, "速さのモードが優先", tool.Name)
		assert.NotEmpty(t, schema.Props["sampling"].Properties["steps"].Description, tool.Name)
		seen++
	}
	assert.Equal(t, 2, seen)
}

func TestTheModelListSaysWhatEachModelTakesAndHowItReadsPromptWeights(t *testing.T) {
	r := newRig(t, gentesttest.WithTemplateModels())
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "image"}, &out)
	byID := map[string]int{}
	for i, m := range out.Models {
		byID[m.ID] = i
	}
	sdxl := out.Models[byID[string(gentesttest.SamplingModel)]]
	require.NotNil(t, sdxl.Sampling)
	assert.Equal(t, 25.0, sdxl.Sampling.Steps.Default)
	assert.Equal(t, 7.0, sdxl.Sampling.CFG.Default)
	assert.Equal(t, "dpmpp_2m", sdxl.Sampling.Sampler.Default)
	assert.Equal(t, "karras", sdxl.Sampling.Scheduler.Default)
	assert.Equal(t, promptweight.Supported, sdxl.PromptWeight)

	cloud := out.Models[byID["nano-banana"]]
	assert.Nil(t, cloud.Sampling, "a cloud model takes none of them")
	assert.Equal(t, promptweight.Unknown, cloud.PromptWeight, "nothing is known of how a cloud model reads weights")
}

func TestARunKeepsTheSeedAndTheValuesItRanWith(t *testing.T) {
	r := newRig(t, gentesttest.WithTemplateModels())
	args := sdxlBatch(map[string]any{"seed": 42, "count": 2, "sampling": map[string]any{"steps": 30, "sampler": "euler"}})
	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	require.NotNil(t, est.Request.Seed)
	assert.EqualValues(t, 42, *est.Request.Seed)
	for _, w := range est.Warnings {
		assert.NotEqual(t, gentest.WarnSamplingIgnored, w.Code, "the model takes all of them")
	}

	out, _ := r.run(args)
	require.Equal(t, "succeeded", string(r.job(out.JobID).State))
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	require.Len(t, b.Results, 2)
	for i, res := range b.Results {
		require.NotNil(t, res.Seed)
		assert.EqualValues(t, 42+i, *res.Seed)
		require.NotNil(t, res.Sampling)
		assert.Equal(t, 30, *res.Sampling.Steps)
		assert.Equal(t, 7.0, *res.Sampling.CFG, "not named: the model's own")
		assert.Equal(t, "euler", res.Sampling.Sampler)
		assert.Equal(t, "karras", res.Sampling.Scheduler)
	}
}

func TestAValueOutOfRangeIsAValidationErrorAndACloudModelWarns(t *testing.T) {
	r := newRig(t, gentesttest.WithTemplateModels())
	assert.Contains(t, r.CallError("estimate_generation_test", sdxlBatch(map[string]any{"sampling": map[string]any{"steps": 500}})), "validation")
	assert.Contains(t, r.CallError("estimate_generation_test", sdxlBatch(map[string]any{"seed": -1})), "validation")

	var est gentest.Estimate
	r.CallOK("estimate_generation_test", map[string]any{
		"media": "image", "models": []string{"nano-banana"}, "prompts": []map[string]any{{"prompt": "x"}}, "sampling": map[string]any{"steps": 30}, "seed": 5,
	}, &est)
	codes := map[string]string{}
	for _, w := range est.Warnings {
		codes[w.Code] = w.Model
	}
	assert.Equal(t, "nano-banana", codes[gentest.WarnSamplingIgnored])
}
