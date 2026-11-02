package gentest_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

const loraObjectInfo = `{"LoraLoaderModelOnly":{"input":{"required":{"lora_name":[["b.safetensors","a.safetensors"],{}]}}}}`

func loraEngine(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(loraObjectInfo)) }))
	t.Cleanup(srv.Close)
	return srv.URL
}

// stageUploads stages n uploads and returns their references as the tool takes them.
func (r *rig) stageUploads(n int) []map[string]any {
	r.T.Helper()
	var out []map[string]any
	for i := range n {
		id, _, err := r.GT.Svc.StageReference(append(append([]byte(nil), gentesttest.PNG...), byte(i)))
		require.NoError(r.T, err)
		out = append(out, map[string]any{"source": "upload", "id": id})
	}
	return out
}

func TestListModelsShowsTheReferenceCapabilitiesAndTheLoRAs(t *testing.T) {
	r := newRig(t, gentesttest.WithReferenceModels(loraEngine(t)))
	var out mcpgentest.ListModelsOutput
	r.CallOK("list_generation_models", map[string]any{"media": "image"}, &out)
	by := map[string]modelcatalog.Info{}
	for _, m := range out.Models {
		by[m.ID] = m
	}
	edit := by[string(gentesttest.EditModel)]
	assert.Equal(t, "edit", edit.ReferenceMode)
	assert.Equal(t, 3, edit.MaxReferenceImages)
	assert.Equal(t, []string{"reference", "face", "style", "edit"}, edit.ReferenceRoles)
	assert.True(t, edit.SupportsLoRA)
	i2i := by[string(gentesttest.Img2ImgModel)]
	assert.Equal(t, "img2img", i2i.ReferenceMode)
	assert.Equal(t, 1, i2i.MaxReferenceImages)
	assert.False(t, i2i.SupportsLoRA)
	assert.Equal(t, []string{"a.safetensors", "b.safetensors"}, out.LoRAs)

	var raw map[string]any
	r.CallOK("list_generation_models", map[string]any{"media": "image"}, &raw)
	assert.Contains(t, raw, "loras")
	first := raw["models"].([]any)[0].(map[string]any)
	assert.Contains(t, first, "accepts_references")
}

func TestListModelsAlwaysHasALoRAList(t *testing.T) {
	r := newRig(t)
	var raw map[string]any
	r.CallOK("list_generation_models", map[string]any{}, &raw)
	assert.Equal(t, []any{}, raw["loras"], "an empty list, not null: the engine is not there")
}

func TestReferencesWithRoleStrengthLoRAAndAvoidPoseRoundTrip(t *testing.T) {
	r := newRig(t, gentesttest.WithReferenceModels(loraEngine(t)))
	refs := r.stageUploads(2)
	refs[0]["role"], refs[0]["strength"] = "face", 0.3
	refs[1]["role"] = "style"
	args := map[string]any{
		"media": "image", "models": []string{string(gentesttest.EditModel), string(gentesttest.Img2ImgModel)},
		"prompts":              []map[string]any{{"prompt": "a fox"}},
		"references":           refs,
		"avoid_reference_pose": true,
		"lora":                 map[string]any{"name": "a.safetensors", "strength": 0.6},
	}

	var est gentest.Estimate
	r.CallOK("estimate_generation_test", args, &est)
	require.Len(t, est.Request.References, 2)
	assert.Equal(t, "face", est.Request.References[0].Role)
	require.NotNil(t, est.Request.References[0].Strength)
	assert.InDelta(t, 0.3, *est.Request.References[0].Strength, 1e-9)
	assert.Nil(t, est.Request.References[1].Strength)
	assert.True(t, est.Request.AvoidReferencePose)
	require.NotNil(t, est.Request.LoRA)
	assert.Equal(t, "a.safetensors", est.Request.LoRA.Name)
	codes := map[string][]string{}
	for _, w := range est.Warnings {
		codes[w.Code] = append(codes[w.Code], w.Model)
	}
	assert.Equal(t, []string{string(gentesttest.Img2ImgModel)}, codes["too_many_refs"])
	assert.Equal(t, []string{string(gentesttest.Img2ImgModel)}, codes["role_unsupported"])
	assert.Equal(t, []string{string(gentesttest.Img2ImgModel)}, codes["lora_unsupported"])
	assert.Equal(t, []string{string(gentesttest.Img2ImgModel)}, codes["negative_folded"], "the pose negative is folded where there is no field")

	run := map[string]any{"estimate_id": est.ID, "confirmed_total_usd": est.TotalUSD}
	for k, v := range args {
		run[k] = v
	}
	var out mcpgentest.RunOutput
	r.CallOK("run_generation_test", run, &out)
	require.Equal(t, "succeeded", string(r.job(out.JobID).State))

	call := r.GT.Fake.CallsOf(gentesttest.EditModel)[0].Image
	require.Len(t, call.Inputs, 2)
	assert.Equal(t, "face", call.Options.Refs[0].Role)
	assert.InDelta(t, 0.3, call.Options.Refs[0].Strength, 1e-9)
	assert.Equal(t, "style", call.Options.Refs[1].Role)
	assert.Equal(t, localcfg.AvoidPoseNegative, call.NegativePrompt)
	require.NotNil(t, call.Options.LoRA)
	assert.Equal(t, localcfg.LoRA{Name: "a.safetensors", Strength: 0.6}, *call.Options.LoRA)

	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	var res mcpgentest.ResultOutput
	r.CallOK("get_generation_result", map[string]any{"result_id": b.Results[0].ID}, &res)
	require.Len(t, res.Inputs, 2)
	assert.Equal(t, "face", res.Inputs[0].Role)
	require.NotNil(t, res.Inputs[0].Strength)
	assert.InDelta(t, 0.3, *res.Inputs[0].Strength, 1e-9)
	assert.True(t, res.AvoidReferencePose)
	require.NotNil(t, res.LoRA)
	assert.InDelta(t, 0.6, res.LoRA.Strength, 1e-9)
}

func TestInvalidRoleStrengthAndLoRAAreValidationErrors(t *testing.T) {
	r := newRig(t, gentesttest.WithReferenceModels(""))
	base := func(mut func(map[string]any)) map[string]any {
		args := map[string]any{"media": "image", "models": []string{string(gentesttest.EditModel)}, "prompts": []map[string]any{{"prompt": "x"}}, "references": r.stageUploads(1)}
		mut(args)
		return args
	}
	for name, mut := range map[string]func(map[string]any){
		"strength 1.5": func(a map[string]any) { a["references"].([]map[string]any)[0]["strength"] = 1.5 },
		"role pose":    func(a map[string]any) { a["references"].([]map[string]any)[0]["role"] = "pose" },
		"empty lora":   func(a map[string]any) { a["lora"] = map[string]any{"name": ""} },
		"lora 2":       func(a map[string]any) { a["lora"] = map[string]any{"name": "a", "strength": 2} },
	} {
		assert.Contains(t, r.CallError("estimate_generation_test", base(mut)), "validation", name)
	}
	video := map[string]any{"media": "video", "models": []string{"kling-3.0"}, "prompts": []map[string]any{{"prompt": "x"}}, "lora": map[string]any{"name": "a"}}
	assert.Contains(t, r.CallError("estimate_generation_test", video), "validation", "a LoRA is for images")
}
