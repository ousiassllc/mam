package configtest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Wan 2.2 14B + the LightX2V LoRA (Issue #930): the workflow is the 4-step one of ComfyUI's official template, and its second speed mode
// puts the usual 20 steps back, so one model serves both.
func TestTheLightX2VTemplateHasTheFastAndTheStandardMode(t *testing.T) {
	tpl, ok := localcfg.TemplateByID("img2video-wan22-14b-lightx2v")
	require.True(t, ok)
	w := tpl.Model.Workflow
	require.Len(t, w.SpeedModes, 2)
	def, _ := w.SpeedMode("")
	assert.Equal(t, "fast", def.ID, "the default is the graph as it stands: 4 steps")
	assert.Empty(t, def.Overrides)
	std, found := w.SpeedMode("standard")
	require.True(t, found)
	assert.NotEmpty(t, std.Overrides)
	_, found = w.SpeedMode("turbo")
	assert.False(t, found)

	// the official template's values: 4 steps, euler / simple, cfg 1, the hand-over after step 2, shift 5, a LoRA on each of the two models
	g := graphOf(t, "img2video-wan22-14b-lightx2v")
	for _, id := range []string{"3", "4"} {
		assert.Equal(t, "4", string(g[id].Inputs["steps"]))
		assert.Equal(t, "1", string(g[id].Inputs["cfg"]))
	}
	assert.Equal(t, "2", string(g["3"].Inputs["end_at_step"]))
	assert.Equal(t, "5", string(g["54"].Inputs["shift"]))
	assert.Equal(t, []string{"97", "98"}, idsOf(g, "LoraLoaderModelOnly"))
	assert.Equal(t, `"wan2.2_i2v_A14b_high_noise_lora_rank64_lightx2v_4step_1022.safetensors"`, string(g["97"].Inputs["lora_name"]))
	assert.Equal(t, `"wan2.2_i2v_A14b_low_noise_lora_rank64_lightx2v_4step_1022.safetensors"`, string(g["98"].Inputs["lora_name"]))
	// the standard mode is the plain img2video-wan22-14b: same unet files, 20 steps, cfg 3.5
	plain := graphOf(t, "img2video-wan22-14b")
	byID := map[string]json.RawMessage{}
	for _, o := range std.Overrides {
		byID[o.Node+"."+o.Input] = o.Value
	}
	assert.JSONEq(t, string(plain["3"].Inputs["steps"]), string(byID["3.steps"]))
	assert.JSONEq(t, string(plain["3"].Inputs["cfg"]), string(byID["3.cfg"]))
	assert.JSONEq(t, string(plain["54"].Inputs["shift"]), string(byID["54.shift"]))
	assert.JSONEq(t, string(plain["3"].Inputs["end_at_step"]), string(byID["3.end_at_step"]))
	assert.Equal(t, string(plain["95"].Inputs["unet_name"]), string(g["95"].Inputs["unet_name"]))
}

func TestOnlyTheDistilledLoRATemplatesHaveSpeedModes(t *testing.T) {
	has := map[string]bool{"img2video-wan22-14b-lightx2v": true, "txt2img-qwen-image-edit-2511": true}
	for _, tpl := range localcfg.Templates() {
		assert.Equal(t, has[tpl.ID], len(tpl.Model.Workflow.SpeedModes) > 0, tpl.ID)
	}
}

// Qwen-Image-Edit 2511 with the Lightning LoRAs (Issue #991): an image workflow has speed modes; the first (the default) is the graph
// itself, the other two put in the LoRA of their step count with cfg 1.
func TestQwenImageEdit2511HasTheStandardAndTheTwoLightningModes(t *testing.T) {
	tpl, ok := localcfg.TemplateByID("txt2img-qwen-image-edit-2511")
	require.True(t, ok)
	w := tpl.Model.Workflow
	var ids []string
	for _, m := range w.SpeedModes {
		ids = append(ids, m.ID)
	}
	assert.Equal(t, []string{"standard", "lightning-8", "lightning-4"}, ids)
	assert.Empty(t, w.SpeedModes[0].Overrides, "the default is the graph itself: 20 steps, cfg 4, no LoRA")
	first, ok := w.SpeedMode("")
	require.True(t, ok)
	assert.Equal(t, "standard", first.ID)
	assert.Empty(t, w.QualityPresets, "the mode owns the steps and the cfg")
	assert.Equal(t, "speed_mode", w.SamplingCaps().Steps.Locked)
	assert.Equal(t, "speed_mode", w.SamplingCaps().CFG.Locked)
	assert.Empty(t, localcfg.ValidateModel("box/x", tpl.Model, comfy()))
}

func TestValidateRefusesBrokenSpeedModes(t *testing.T) {
	mode := func(id string, overrides ...localcfg.Override) localcfg.SpeedMode {
		return localcfg.SpeedMode{ID: id, Label: id, Overrides: overrides}
	}
	set := func(node, input, value string) localcfg.Override {
		return localcfg.Override{Node: node, Input: input, Value: json.RawMessage(value)}
	}
	for name, modes := range map[string][]localcfg.SpeedMode{
		"an ID with a capital":           {mode("Fast")},
		"an empty ID":                    {mode("")},
		"a repeated ID":                  {mode("fast"), mode("fast")},
		"no label":                       {{ID: "fast"}},
		"overrides on the first mode":    {mode("fast", set("3", "steps", "4"))},
		"an override of a missing node":  {mode("fast"), mode("slow", set("999", "steps", "20"))},
		"an override of a missing input": {mode("fast"), mode("slow", set("3", "nope", "20"))},
		"an override that is not JSON":   {mode("fast"), mode("slow", set("3", "steps", "twenty"))},
		"an override linking to no node": {mode("fast"), mode("slow", set("54", "model", `["404",0]`))},
	} {
		m := withModes(t, "img2video-wan22-14b-lightx2v", modes)
		assert.NotEmpty(t, localcfg.ValidateModel("box/x", m, comfy()), name)
	}
	for name, modes := range map[string][]localcfg.SpeedMode{
		"an image workflow's override of a missing node": {mode("standard"), mode("fast", set("999", "steps", "4"))},
		"an image workflow's override on the first mode": {mode("standard", set("3", "steps", "4"))},
	} {
		assert.NotEmpty(t, localcfg.ValidateModel("box/x", withModes(t, "txt2img-qwen-image-edit-2511", modes), comfy()), name)
	}
	image := withModes(t, "txt2img-qwen-image-edit-2511", []localcfg.SpeedMode{mode("standard"), mode("fast", set("3", "steps", "4"))})
	assert.Empty(t, localcfg.ValidateModel("box/x", image, comfy()), "an image workflow may have speed modes")
}

// withModes is the model of a template with its speed modes replaced (a copy: the template stays as it was).
func withModes(t *testing.T, id string, modes []localcfg.SpeedMode) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	data, err := json.Marshal(tpl.Model)
	require.NoError(t, err)
	var m localcfg.Model
	require.NoError(t, json.Unmarshal(data, &m))
	m.Workflow.SpeedModes = modes
	return m
}

// SpeedModeReads is the files a speed mode's graph asks the server to load (#1025): the nodes behind the output node, with the mode's overrides put in. The LoRA node
// of another mode is not behind the output, so the standard mode reads neither Lightning file and each Lightning mode reads only its own.
func TestSpeedModeReadsIsTheFilesTheModesGraphLoads(t *testing.T) {
	tpl, ok := localcfg.TemplateByID("txt2img-qwen-image-edit-2511")
	require.True(t, ok)
	w := tpl.Model.Workflow
	const l4, l8 = "Qwen-Image-Edit-2511-Lightning-4steps-V1.0-bf16.safetensors", "Qwen-Image-Edit-2511-Lightning-8steps-V1.0-bf16.safetensors"
	both := []string{l4, l8, "qwen_image_edit_2511_fp8mixed.safetensors"}

	assert.Equal(t, []string{"qwen_image_edit_2511_fp8mixed.safetensors"}, w.SpeedModeReads("standard", both), "no LoRA node reaches the output")
	assert.Equal(t, []string{"qwen_image_edit_2511_fp8mixed.safetensors"}, w.SpeedModeReads("", both), "the empty id is the first mode")
	assert.Equal(t, []string{l4, "qwen_image_edit_2511_fp8mixed.safetensors"}, w.SpeedModeReads("lightning-4", both))
	assert.Equal(t, []string{l8, "qwen_image_edit_2511_fp8mixed.safetensors"}, w.SpeedModeReads("lightning-8", both))
	assert.Equal(t, []string{l4}, w.SpeedModeReads("lightning-4", []string{l4, l8}), "only the names asked for")
	assert.Empty(t, w.SpeedModeReads("lightning-4", nil))
	assert.Empty(t, w.SpeedModeReads("no-such-mode", both), "a mode the workflow does not have reads nothing")
}
