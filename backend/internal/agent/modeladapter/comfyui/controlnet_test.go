package comfyui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
)

// A ControlNet workflow (Issue #976) takes the reference image as its control image and the strength of the reference as the
// ControlNet's strength: the picture is uploaded, the LoadImage node names it, ControlNetApplyAdvanced gets the strength, and the sampler
// reads the conditioning that comes out of the ControlNet, not the one of the text encoders.
func TestControlNetWorkflowTakesTheReferenceAsTheControlImageAndItsStrength(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl-controlnet", nil)

	_, err := b.GenerateImage(ctx(t), "", refs("a cat", ref("", 0.35)))
	require.NoError(t, err)

	g := f.Graph()
	assert.Equal(t, "a cat", str(g["6"].Inputs["text"]))
	assert.Regexp(t, `\.png$`, str(g["10"].Inputs["image"]), "the control image is the uploaded file")
	assert.Equal(t, 1, f.Count("POST /upload/image"))
	assert.InDelta(t, 0.35, num(g["13"].Inputs["strength"]), 1e-9)
	assert.Equal(t, `"10"/0`, linkTo(g["13"].Inputs["image"]))
	assert.Equal(t, `"12"/0`, linkTo(g["13"].Inputs["control_net"]))
	assert.Equal(t, `"13"/0`, linkTo(g["3"].Inputs["positive"]))
	assert.Equal(t, `"13"/1`, linkTo(g["3"].Inputs["negative"]))
	assert.Equal(t, "xinsir-controlnet-canny-sdxl-1.0.safetensors", str(g["12"].Inputs["control_net_name"]))
}

func TestControlNetStrengthDefaultsAndIsKeptWithinZeroAndOne(t *testing.T) {
	for strength, want := range map[float64]float64{0: 0.7, 0.9: 0.9, 5: 1} {
		f := comfyuitest.New(t)
		b := backend(t, f, "txt2img-sdxl-controlnet", nil)
		_, err := b.GenerateImage(ctx(t), "", refs("x", ref("", strength)))
		require.NoError(t, err)
		assert.InDelta(t, want, num(f.Graph()["13"].Inputs["strength"]), 1e-9, "strength %v", strength)
	}
}

// Without the control image the ControlNet has nothing to follow: the run is refused before anything is sent, with a sentence that says what
// the picture is for (the generation test shows it), instead of running as a plain text-to-image.
func TestControlNetAndIPAdapterWorkflowsRefuseARunWithoutTheirImage(t *testing.T) {
	for template, want := range map[string]string{
		"txt2img-sdxl-controlnet": "control image of its ControlNet",
		"txt2img-sdxl-ipadapter":  "its IP-Adapter takes the look from",
	} {
		f := comfyuitest.New(t)
		b := backend(t, f, template, nil)
		_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x", AspectRatio: "1:1"})
		require.Error(t, err, template)
		assert.Contains(t, err.Error(), want, template)
		assert.Zero(t, f.Count("POST /prompt"), "nothing is queued on the server")
	}
}

// The IP-Adapter workflow gets the reference image at its LoadImage and its strength as the adapter's weight; the model the sampler reads is
// the adapter's output.
func TestIPAdapterWorkflowTakesTheReferenceAsTheStyleImageAndItsWeight(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl-ipadapter", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("a cat", ref("", 0.5)))
	require.NoError(t, err)
	g := f.Graph()
	assert.InDelta(t, 0.5, num(g["13"].Inputs["weight"]), 1e-9)
	assert.Equal(t, `"10"/0`, linkTo(g["13"].Inputs["image"]))
	assert.Equal(t, `"13"/0`, linkTo(g["3"].Inputs["model"]))
	assert.Equal(t, 1, f.Count("POST /upload/image"))
}
