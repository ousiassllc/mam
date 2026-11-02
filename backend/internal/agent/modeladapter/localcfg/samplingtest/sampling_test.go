package samplingtest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/promptweight"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
)

func template(t *testing.T, id string) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return tpl.Model
}

// A workflow tells the screen what it takes and what it starts from: the literals of its graph are the defaults.
func TestSamplingCapsAreTheValuesTheGraphHolds(t *testing.T) {
	c := template(t, "txt2img-sdxl").Workflow.SamplingCaps()
	require.NotNil(t, c)
	assert.Equal(t, &sampling.NumberCap{Default: 25}, c.Steps)
	assert.Equal(t, &sampling.NumberCap{Default: 7}, c.CFG)
	assert.Equal(t, &sampling.NameCap{Default: "dpmpp_2m"}, c.Sampler)
	assert.Equal(t, &sampling.NameCap{Default: "karras"}, c.Scheduler)

	// FLUX.2 klein has a Flux2Scheduler (steps only), a CFGGuider and a KSamplerSelect: no scheduler to pick.
	k := template(t, "txt2img-flux2-klein").Workflow.SamplingCaps()
	require.NotNil(t, k)
	assert.Equal(t, 4.0, k.Steps.Default)
	assert.Equal(t, 1.0, k.CFG.Default)
	assert.Equal(t, "euler", k.Sampler.Default)
	assert.Nil(t, k.Scheduler)
}

// A distilled LoRA is only right at its own steps and cfg: a workflow with speed modes owns them, so a request's are not taken.
func TestSpeedModesLockTheStepsAndTheCFG(t *testing.T) {
	c := template(t, "img2video-wan22-14b-lightx2v").Workflow.SamplingCaps()
	require.NotNil(t, c)
	assert.Equal(t, sampling.LockedBySpeedMode, c.Steps.Locked)
	assert.Equal(t, sampling.LockedBySpeedMode, c.CFG.Locked)
	assert.NotEmpty(t, c.Sampler.Default, "the sampler is not owned by a mode")

	steps, cfg := 30, 7.0
	got := c.Applicable(sampling.Settings{Steps: &steps, CFG: &cfg, Sampler: "heun", Scheduler: "karras"})
	assert.Nil(t, got.Steps)
	assert.Nil(t, got.CFG)
	assert.Equal(t, "heun", got.Sampler)
	assert.Equal(t, "karras", got.Scheduler)
	assert.Equal(t, []string{"ステップ数", "CFG"}, c.Ignored(sampling.Settings{Steps: &steps, CFG: &cfg, Sampler: "heun"}))

	// the same workflow without modes (the uncensored remix of it) takes them
	plain := template(t, "img2video-wan22-14b-lightx2v-uncensored").Workflow.SamplingCaps()
	assert.Empty(t, plain.Steps.Locked)
}

func TestAWorkflowThatTakesNoSamplingValueSaysSo(t *testing.T) {
	for _, id := range []string{"txt2video-ltx-2-5", "img2video-ltx-2-5-nvfp4"} {
		w := template(t, id).Workflow
		assert.Nil(t, w.SamplingCaps(), id+": the sigmas of a distilled LTX-2.5 are fixed in the graph")
		steps := 8
		assert.Equal(t, []string{"ステップ数"}, w.SamplingCaps().Ignored(sampling.Settings{Steps: &steps}))
		assert.Nil(t, w.SamplingCaps().Effective(sampling.Settings{Steps: &steps}))
	}
}

// What a result keeps: the value of the request where the workflow takes it, else the workflow's own.
func TestEffectiveIsWhatTheRunUsed(t *testing.T) {
	c := template(t, "txt2img-sdxl").Workflow.SamplingCaps()
	steps := 40
	got := c.Effective(sampling.Settings{Steps: &steps, Sampler: "euler"})
	require.NotNil(t, got.Steps)
	assert.Equal(t, 40, *got.Steps)
	assert.Equal(t, 7.0, *got.CFG)
	assert.Equal(t, "euler", got.Sampler)
	assert.Equal(t, "karras", got.Scheduler)
}

func TestSettingsAreCheckedAgainstTheRanges(t *testing.T) {
	steps := func(n int) *int { return &n }
	cfg := func(f float64) *float64 { return &f }
	assert.NoError(t, sampling.Settings{}.Validate())
	assert.NoError(t, sampling.Settings{Steps: steps(1), CFG: cfg(0), Sampler: "dpmpp_2m", Scheduler: "karras"}.Validate())
	assert.NoError(t, sampling.Settings{Steps: steps(150), CFG: cfg(30)}.Validate())
	for name, s := range map[string]sampling.Settings{
		"steps 0":           {Steps: steps(0)},
		"steps 151":         {Steps: steps(151)},
		"cfg negative":      {CFG: cfg(-0.1)},
		"cfg 30.1":          {CFG: cfg(30.1)},
		"unknown sampler":   {Sampler: "my_sampler"},
		"unknown scheduler": {Scheduler: "zigzag"},
	} {
		assert.Error(t, s.Validate(), name)
	}
	var nan = 0.0
	nan = nan / nan
	assert.Error(t, sampling.Settings{CFG: &nan}.Validate(), "NaN is not a cfg")
}

func TestSamplingAssignmentsAreCheckedAgainstTheGraph(t *testing.T) {
	validate := func(edit func(m *localcfg.Model)) []error {
		m := template(t, "txt2img-sdxl")
		w := *m.Workflow
		m.Workflow = &w
		edit(&m)
		return localcfg.ValidateSaved(m)
	}
	assert.Empty(t, validate(func(*localcfg.Model) {}))

	errs := validate(func(m *localcfg.Model) { // steps into a text input
		m.Workflow.Assignments = append(m.Workflow.Assignments, localcfg.Assignment{Param: localcfg.ParamSteps, Node: "4", Input: "ckpt_name"})
	})
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "number input")

	errs = validate(func(m *localcfg.Model) { // the sampler into a number input
		m.Workflow.Assignments = append(m.Workflow.Assignments, localcfg.Assignment{Param: localcfg.ParamSampler, Node: "5", Input: "batch_size"})
	})
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[0].Error(), "text input")

	errs = validate(func(m *localcfg.Model) { // a ratio on the cfg
		m.Workflow.Assignments = append(m.Workflow.Assignments, localcfg.Assignment{Param: localcfg.ParamCFG, Node: "5", Input: "batch_size", Ratio: 0.5})
	})
	require.NotEmpty(t, errs)
	assert.Contains(t, errs[len(errs)-1].Error(), "ratio")

	errs = validate(func(m *localcfg.Model) { // a ratio of 1 hands over nothing
		m.Workflow.Assignments = append(m.Workflow.Assignments, localcfg.Assignment{Param: localcfg.ParamSteps, Node: "5", Input: "batch_size", Ratio: 1})
	})
	assert.NotEmpty(t, errs)
}

// The templates say how their model reads the weights of a prompt; the claim of each follows from the text encoder (package promptweight).
func TestEveryTemplateSaysWhetherItsModelReadsPromptWeights(t *testing.T) {
	want := map[string]promptweight.Level{
		"txt2img-sdxl": promptweight.Supported, "img2img-sdxl": promptweight.Supported, "txt2img-sd35-medium": promptweight.Supported,
		"txt2img-flux-dev": promptweight.Weak, "txt2img-flux-schnell": promptweight.Weak, "txt2img-chroma": promptweight.Weak,
		"txt2img-qwen-image": promptweight.Unsupported, "txt2img-qwen-image21": promptweight.Unsupported,
		"txt2img-flux2-klein": promptweight.Unsupported, "txt2video-ltx-2-5": promptweight.Unsupported,
		"txt2video-wan22-5b": promptweight.Weak, "txt2video-hunyuan15-480p": promptweight.Unsupported,
	}
	for _, tpl := range localcfg.Templates() {
		got := tpl.Model.Workflow.PromptWeight
		assert.True(t, got.Valid(), "%s says %q", tpl.ID, got)
		if w, ok := want[tpl.ID]; ok {
			assert.Equal(t, w, got, tpl.ID)
		}
	}
}

func TestAnUnknownPromptWeightIsRefused(t *testing.T) {
	m := template(t, "txt2img-sdxl")
	w := *m.Workflow
	w.PromptWeight = "strong"
	m.Workflow = &w
	errs := localcfg.ValidateSaved(m)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "prompt_weight")
	assert.Equal(t, promptweight.Unknown, promptweight.Level("").OrUnknown())

	// a template written before the field existed still reads: the field is optional in the JSON
	var back localcfg.Workflow
	require.NoError(t, json.Unmarshal([]byte(`{"graph":{},"assignments":[],"output_node":"1"}`), &back))
	assert.Empty(t, back.PromptWeight)
}
