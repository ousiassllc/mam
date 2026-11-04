package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// The Mega merge's first-frame mode (Issue #1028). The author's WanVideoVACEStartToEndFrame (a WanVideoWrapper custom node) makes a control video that
// is the first frame followed by gray frames and a mask that is 0 on the first frame and 1 elsewhere. WanVaceToVideo pads a control video shorter than
// its length with 0.5 and a mask shorter than its length with 1, so the first frame itself is the control video and a one-frame mask of 0 (SolidMask)
// is the mask: the same two inputs with core nodes only (the numbers were compared with the author's node for 49, 65 and 81 frames on 2026-11-04).
// The rest is the Mega text to video graph with the control strength 1.
func TestWan14BRapidMegaI2VTemplateGivesTheFirstFrameAsTheControlVideoWithAOneFrameMask(t *testing.T) {
	const id = "img2video-wan22-14b-rapid-mega"
	g := graphOf(t, id)
	assert.Equal(t, []string{"26"}, idsOf(g, "CheckpointLoaderSimple"))
	for _, class := range []string{"UNETLoader", "CLIPLoader", "VAELoader", "CLIPVisionLoader", "WanImageToVideo"} {
		assert.Empty(t, idsOf(g, class), class)
	}
	assert.Equal(t, "1", inputOf(g, "28", "strength"), "the control is applied: this is not the text to video")
	from, n := linked(t, g, "28", "control_video")
	assert.Equal(t, "LoadImage", n.ClassType, "the first frame itself is the control video: "+from)
	from, n = linked(t, g, "28", "control_masks")
	assert.Equal(t, "SolidMask", n.ClassType)
	assert.Equal(t, "0", inputOf(g, from, "value"), "the first frame is kept (mask 0); WanVaceToVideo pads the rest with 1")
	assert.NotContains(t, g["28"].Inputs, "reference_image")

	// the Mega text to video graph with the same sampling: only the control inputs and the strength differ
	assert.Equal(t, graphWithout(t, "txt2video-wan22-14b-rapid-mega", "28", "3"), graphWithout(t, id, "28", "3", "56", "60"))
	for _, in := range []string{"steps", "cfg", "sampler_name", "scheduler", "model", "latent_image", "positive", "negative"} {
		assert.Equal(t, inputOf(graphOf(t, "txt2video-wan22-14b-rapid-mega"), "3", in), inputOf(g, "3", in), in)
	}
	assert.JSONEq(t, `["28",3]`, inputOf(g, "29", "trim_amount"))

	tpl, _ := localcfg.TemplateByID(id)
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.False(t, caps.LastFrame, "the two-frame mode needs a gray frame count that follows the clip length, which the assignments cannot express")
	// the mask has the size of the video: width and height go to the mask too (so the size multiple and the resolution reach both)
	var maskSize int
	for _, a := range tpl.Model.Workflow.Assignments {
		if a.Node == "60" && (a.Param == localcfg.ParamWidth || a.Param == localcfg.ParamHeight) {
			maskSize++
		}
	}
	assert.Equal(t, 2, maskSize)
}

// WAN22.XX Palingenesis (Issue #1028) is the official Wan 2.2 14B text to video workflow with the two experts and the text encoder swapped: nothing
// else of the graph changes (the author's own workflows are WanVideoWrapper ones and state no sampler values in the repository).
func TestWan14BPalingenesisTemplateIsTheOfficialWorkflowWithOtherFiles(t *testing.T) {
	const id = "txt2video-wan22-14b-palingenesis"
	g := graphOf(t, id)
	assert.Equal(t, `"WAN22.XX_Palingenesis_high_t2v_final.safetensors"`, inputOf(g, "95", "unet_name"))
	assert.Equal(t, `"WAN22.XX_Palingenesis_low_t2v_final.safetensors"`, inputOf(g, "96", "unet_name"))
	assert.Equal(t, `"nsfw_wan_umt5-xxl_bf16_fixed2.safetensors"`, inputOf(g, "38", "clip_name"))
	assert.Equal(t, `"wan"`, inputOf(g, "38", "type"), "the standard CLIPLoader of type wan reads the text encoder (its keys are the HF T5 ones)")
	assert.Equal(t, graphWithout(t, "txt2video-wan22-14b", "95", "96", "38"), graphWithout(t, id, "95", "96", "38"))
	tpl, _ := localcfg.TemplateByID(id)
	base, _ := localcfg.TemplateByID("txt2video-wan22-14b")
	assert.Equal(t, base.Model.Workflow.Assignments, tpl.Model.Workflow.Assignments)
	caps, _ := tpl.Model.VideoCaps()
	assert.False(t, caps.FirstFrame, "text to video")
}
