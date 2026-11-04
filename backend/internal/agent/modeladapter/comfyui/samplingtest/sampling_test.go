package samplingtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

func ptr[T any](v T) *T { return &v }

// A request that names no sampling value runs every bundled workflow exactly as it was before the assignments existed: the graph the
// server gets is the same, node for node and input for input. (A video enhancement workflow is no generation: it takes a source video, not a
// request of the Adapter's generation calls, and has no sampling value of the user's; enhancetest runs it.)
func TestNamingNothingSendsTheSameGraphForEveryTemplate(t *testing.T) {
	for _, tp := range localcfg.Templates() {
		if tp.Model.IsVideoEnhance() {
			continue
		}
		t.Run(tp.ID, func(t *testing.T) {
			assert.Equal(t, run(t, tp, without, localcfg.SamplingSettings{}, ""), run(t, tp, nil, localcfg.SamplingSettings{}, ""))
		})
	}
}

// What the user sets reaches the node the assignment names, and only that: each case lists every input that differs from the
// default run.
func TestTheValuesOfARequestReachTheNodes(t *testing.T) {
	cases := []struct {
		name, template string
		set            localcfg.SamplingSettings
		want           map[string]string // "node.input" -> JSON
	}{
		{"KSampler", "txt2img-sdxl", localcfg.SamplingSettings{Steps: ptr(40), CFG: ptr(5.5), Sampler: "euler", Scheduler: "normal"},
			map[string]string{"3.steps": "40", "3.cfg": "5.5", "3.sampler_name": `"euler"`, "3.scheduler": `"normal"`}},
		{"KSampler, only the steps", "txt2img-flux-dev", localcfg.SamplingSettings{Steps: ptr(30)}, map[string]string{"3.steps": "30"}},
		{"BetaSamplingScheduler, CFGGuider and KSamplerSelect", "txt2img-chroma", localcfg.SamplingSettings{Steps: ptr(32), CFG: ptr(4.2), Sampler: "dpmpp_2m"},
			map[string]string{"751.steps": "32", "694.cfg": "4.2", "700.sampler_name": `"dpmpp_2m"`}},
		{"Flux2Scheduler, CFGGuider and KSamplerSelect", "txt2img-flux2-klein", localcfg.SamplingSettings{Steps: ptr(6), CFG: ptr(1.5), Sampler: "heun"},
			map[string]string{"62.steps": "6", "63.cfg": "1.5", "61.sampler_name": `"heun"`}},
		{"a scheduler for a workflow whose scheduler is not a name", "txt2img-flux2-klein", localcfg.SamplingSettings{Scheduler: "karras"}, nil},
		{"BasicScheduler", "txt2video-hunyuan15-480p", localcfg.SamplingSettings{Steps: ptr(25), Scheduler: "karras", CFG: ptr(2.0)},
			map[string]string{"128.steps": "25", "128.scheduler": `"karras"`, "131.cfg": "2"}},
		{"LTXVScheduler and SamplerCustom", "txt2video-ltx-v095", localcfg.SamplingSettings{Steps: ptr(20), CFG: ptr(4.0), Sampler: "euler_ancestral"},
			map[string]string{"71.steps": "20", "72.cfg": "4", "73.sampler_name": `"euler_ancestral"`}},
		{"KSampler of a video", "txt2video-wan22-5b", localcfg.SamplingSettings{Steps: ptr(10), CFG: ptr(4.0)}, map[string]string{"3.steps": "10", "3.cfg": "4"}},
		// two KSamplerAdvanced: the high noise model runs steps 0..handover and the low noise one the rest; the handover keeps its
		// place (half of the steps) when the steps change
		{"KSamplerAdvanced, two stages", "img2video-wan22-14b", localcfg.SamplingSettings{Steps: ptr(30), CFG: ptr(4.0), Sampler: "dpmpp_2m", Scheduler: "karras"},
			map[string]string{
				"3.steps": "30", "4.steps": "30", "3.cfg": "4", "4.cfg": "4", "3.sampler_name": `"dpmpp_2m"`, "4.sampler_name": `"dpmpp_2m"`,
				"3.scheduler": `"karras"`, "4.scheduler": `"karras"`, "3.end_at_step": "15", "4.start_at_step": "15",
			}},
		{"the handover of a remix keeps its place", "img2video-wan22-14b-remix", localcfg.SamplingSettings{Steps: ptr(24)},
			map[string]string{"3.steps": "24", "4.steps": "24", "3.end_at_step": "20", "4.start_at_step": "20"}},
		{"two steps hand over after the first", "img2video-wan22-14b-lightx2v-uncensored", localcfg.SamplingSettings{Steps: ptr(2)},
			map[string]string{"3.steps": "2", "4.steps": "2", "3.end_at_step": "1", "4.start_at_step": "1"}},
		// the distilled LTX-2.5 runs on fixed sigmas: it takes none of them
		{"a workflow without assignments", "txt2video-ltx-2-5", localcfg.SamplingSettings{Steps: ptr(30), CFG: ptr(5.0), Sampler: "heun", Scheduler: "karras"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tp := tpl(t, c.template)
			base, got := run(t, tp, nil, localcfg.SamplingSettings{}, ""), run(t, tp, nil, c.set, "")
			diff := map[string]string{}
			for id, node := range got {
				for in, v := range node {
					if base[id][in] != v {
						diff[id+"."+in] = v
					}
				}
			}
			if c.want == nil {
				assert.Empty(t, diff)
				return
			}
			assert.Equal(t, c.want, diff)
		})
	}
}

// A distilled LoRA is only right at its own steps and cfg: with speed modes the mode wins and the steps and the cfg of a request are
// not sent, in either mode. The sampler is not the mode's, and goes in.
func TestASpeedModeOwnsTheStepsAndTheCFG(t *testing.T) {
	tp := tpl(t, "img2video-wan22-14b-lightx2v")
	set := localcfg.SamplingSettings{Steps: ptr(30), CFG: ptr(7.0), Sampler: "heun"}
	for _, mode := range []string{"fast", "standard"} {
		base, got := run(t, tp, nil, localcfg.SamplingSettings{}, mode), run(t, tp, nil, set, mode)
		for _, id := range []string{"3", "4"} {
			assert.Equal(t, base[id]["steps"], got[id]["steps"], mode+" "+id)
			assert.Equal(t, base[id]["cfg"], got[id]["cfg"], mode+" "+id)
			assert.Equal(t, `"heun"`, got[id]["sampler_name"], mode+" "+id)
			assert.Equal(t, base[id]["start_at_step"], got[id]["start_at_step"], mode+" "+id)
		}
	}
	fast := run(t, tp, nil, localcfg.SamplingSettings{}, "fast")
	assert.Equal(t, "4", fast["3"]["steps"])
	assert.Equal(t, "1", fast["3"]["cfg"])
}

// The sampling of a reference edit that is cut into parts follows the steps of the request (the cut is a share of them).
func TestAReferenceEditSamplesTheStepsOfTheRequest(t *testing.T) {
	tp := tpl(t, "txt2img-qwen-image21")
	got := run(t, tp, nil, localcfg.SamplingSettings{Steps: ptr(10), CFG: ptr(2.0)}, "")
	assert.Equal(t, "10", got["3"]["steps"])
	assert.Equal(t, "2", got["3"]["cfg"])
}

// A two-stage sampler (Wan 2.2) hands over after at least one step and the second stage needs one more: one step would leave the low noise
// model nothing to do (a video of noise), so the run is refused before it is queued, and two steps run both stages.
func TestATwoStageWorkflowRefusesOneStep(t *testing.T) {
	tp := tpl(t, "img2video-wan22-14b")
	_, err := comfyui.GraphFor(*tp.Model.Workflow, modeladapter.ImageRequest{Prompt: "x", Options: localcfg.ImageOptions{Sampling: localcfg.SamplingSettings{Steps: ptr(1)}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2")
	raw, err := comfyui.GraphFor(*tp.Model.Workflow, modeladapter.ImageRequest{Prompt: "x", Options: localcfg.ImageOptions{Sampling: localcfg.SamplingSettings{Steps: ptr(2)}}})
	require.NoError(t, err)
	g, err := localcfg.ParseGraph(raw)
	require.NoError(t, err)
	assert.Equal(t, "1", string(g["3"].Inputs["end_at_step"]), "the first stage runs one step")
	assert.Equal(t, "1", string(g["4"].Inputs["start_at_step"]), "the second stage runs the other")
}
