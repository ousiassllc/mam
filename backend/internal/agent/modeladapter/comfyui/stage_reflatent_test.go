package comfyui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
)

// FLUX.2 klein (Issue #931): a run without a reference is the text-to-image workflow. The four slots (the image, its scaler, its
// VAEEncode and the two ReferenceLatent nodes) all go, and the guider takes the prompt and the zeroed negative as they are.
func TestKleinWithoutAReferenceIsTextToImage(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-flux2-klein", nil)
	seed := int64(5)
	_, err := b.GenerateImage(ctx(t), "", withSeed(modeladapter.ImageRequest{Prompt: "a cat", AspectRatio: "16:9"}, &seed))
	require.NoError(t, err)
	g := f.Graph()
	for id, n := range g {
		assert.NotContains(t, []string{"LoadImage", "ImageScaleToTotalPixels", "VAEEncode", "ReferenceLatent"}, n.ClassType, "node %s", id)
	}
	assert.Equal(t, `"6"/0`, linkTo(g["63"].Inputs["positive"]))
	assert.Equal(t, `"7"/0`, linkTo(g["63"].Inputs["negative"]))
	assert.Equal(t, "a cat", str(g["6"].Inputs["text"]))
	assert.Equal(t, 5.0, num(g["73"].Inputs["noise_seed"]))
	for _, id := range []string{"5", "62"} {
		assert.Equal(t, 1344.0, num(g[id].Inputs["width"]), id)
		assert.Equal(t, 768.0, num(g[id].Inputs["height"]), id)
	}
	assert.Equal(t, 4.0, num(g["62"].Inputs["steps"]))
	assert.Equal(t, 1.0, num(g["63"].Inputs["cfg"]), "the distilled model runs without guidance")
	assert.Equal(t, 0, f.Count("POST /upload/image"))
}

// Two references: the slots of the others are gone, each reference is a ReferenceLatent in the positive and in the negative
// chain, in the order of the references, and the prompt says "image N" for the <imageN> of the screen.
func TestKleinChainsAReferenceLatentForEachReference(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-flux2-klein", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("<image2> in the pose of <image1>", ref("edit", 1), ref("", 1)))
	require.NoError(t, err)
	g := f.Graph()

	assert.Equal(t, `"132"/0`, linkTo(g["63"].Inputs["positive"]))
	assert.Equal(t, `"131"/0`, linkTo(g["132"].Inputs["conditioning"]))
	assert.Equal(t, `"6"/0`, linkTo(g["131"].Inputs["conditioning"]))
	assert.Equal(t, `"142"/0`, linkTo(g["63"].Inputs["negative"]))
	assert.Equal(t, `"141"/0`, linkTo(g["142"].Inputs["conditioning"]))
	assert.Equal(t, `"7"/0`, linkTo(g["141"].Inputs["conditioning"]))
	for ref, latent := range map[string]string{"131": "121", "141": "121", "132": "122", "142": "122"} {
		assert.Equal(t, `"`+latent+`"/0`, linkTo(g[ref].Inputs["latent"]), ref)
	}
	assert.Equal(t, `"101"/0`, linkTo(g["111"].Inputs["image"]))
	assert.Equal(t, `"39"/0`, linkTo(g["121"].Inputs["vae"]))
	for _, id := range []string{"103", "113", "123", "133", "143", "104", "114", "124", "134", "144"} {
		_, found := g[id]
		assert.False(t, found, "node %s is the slot of a third or fourth reference", id)
	}
	assert.Len(t, f.Uploads(), 2)
	assert.Equal(t, "image 2 in the pose of image 1", str(g["6"].Inputs["text"]))
	_, staged := g["mam_split_0"]
	assert.False(t, staged, "strength 1 keeps both to the end: one sampling")
}

// More references than the four slots: the rest is not uploaded.
func TestKleinTakesFourReferencesAtMost(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-flux2-klein", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("x", ref("", 1), ref("", 1), ref("", 1), ref("", 1), ref("", 1)))
	require.NoError(t, err)
	assert.Len(t, f.Uploads(), 4)
	assert.Equal(t, `"134"/0`, linkTo(f.Graph()["63"].Inputs["positive"]))
}

// A weaker reference leaves the sampling earlier (4 steps: strength 0.5 is the first 2). The part after it has its own text
// encoder, negative, chains and guider with the references still in it, and the prompt is renumbered.
func TestKleinWeakerReferenceLeavesTheSamplingEarlier(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-flux2-klein", nil)
	seed := int64(77)
	_, err := b.GenerateImage(ctx(t), "", withSeed(refs("<image1> next to <image2>", ref("edit", 0.5), ref("edit", 1)), &seed))
	require.NoError(t, err)
	g := f.Graph()

	assert.Equal(t, 2.0, num(g["mam_split_0"].Inputs["step"]))
	assert.Equal(t, `"62"/0`, linkTo(g["mam_split_0"].Inputs["sigmas"]))
	assert.Equal(t, "image 1 next to image 2", str(g["mam_enc_0"].Inputs["text"]))
	assert.Equal(t, "the reference image next to image 1", str(g["mam_enc_1"].Inputs["text"]))

	// the first part has both references, the second the second one only
	assert.Equal(t, `"mam_pos_0_1"/0`, linkTo(g["mam_guider_0"].Inputs["positive"]))
	assert.Equal(t, `"mam_pos_0_0"/0`, linkTo(g["mam_pos_0_1"].Inputs["conditioning"]))
	assert.Equal(t, `"mam_enc_0"/0`, linkTo(g["mam_pos_0_0"].Inputs["conditioning"]))
	assert.Equal(t, `"121"/0`, linkTo(g["mam_pos_0_0"].Inputs["latent"]))
	assert.Equal(t, `"mam_pos_1_0"/0`, linkTo(g["mam_guider_1"].Inputs["positive"]))
	assert.Equal(t, `"122"/0`, linkTo(g["mam_pos_1_0"].Inputs["latent"]), "the second reference is the one left")
	assert.Equal(t, `"mam_negref_1_0"/0`, linkTo(g["mam_guider_1"].Inputs["negative"]))
	assert.Equal(t, `"mam_neg_1"/0`, linkTo(g["mam_negref_1_0"].Inputs["conditioning"]))
	assert.Equal(t, `"mam_enc_1"/0`, linkTo(g["mam_neg_1"].Inputs["conditioning"]), "the zeroed negative follows its part's prompt")
	_, nodeWithoutPart := g["mam_pos_1_1"]
	assert.False(t, nodeWithoutPart)

	// the samplings run one after the other on the same latent, the noise only in the first
	assert.Equal(t, `"5"/0`, linkTo(g["mam_run_0"].Inputs["latent_image"]))
	assert.Equal(t, `"mam_run_0"/0`, linkTo(g["mam_run_1"].Inputs["latent_image"]))
	assert.Equal(t, `"73"/0`, linkTo(g["mam_run_0"].Inputs["noise"]))
	assert.Equal(t, `"mam_nonoise"/0`, linkTo(g["mam_run_1"].Inputs["noise"]))
	assert.Equal(t, 77.0, num(g["73"].Inputs["noise_seed"]))
	assert.Equal(t, `"mam_run_1"/0`, linkTo(g["65"].Inputs["samples"]))
	for _, id := range []string{"64", "63", "6", "7", "131", "132", "141", "142"} {
		_, kept := g[id]
		assert.False(t, kept, "node %s is replaced by the parts", id)
	}
}

// A reference that drops out in the last step leaves a part with no reference at all: its guider takes the prompt and the
// negative as they are, and a face reference is told to the model in the prompt (it has no vision tower to read a face).
func TestKleinVeryWeakFaceReferenceIsGoneAfterItsFirstStep(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-flux2-klein", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("a woman on a beach", ref("face", 0.25)))
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, 1.0, num(g["mam_split_0"].Inputs["step"]))
	assert.Contains(t, str(g["mam_enc_0"].Inputs["text"]), "Keep only the face and identity of image 1")
	assert.Equal(t, "a woman on a beach", str(g["mam_enc_1"].Inputs["text"]))
	assert.Equal(t, `"mam_enc_1"/0`, linkTo(g["mam_guider_1"].Inputs["positive"]))
	assert.Equal(t, `"mam_neg_1"/0`, linkTo(g["mam_guider_1"].Inputs["negative"]))
}
