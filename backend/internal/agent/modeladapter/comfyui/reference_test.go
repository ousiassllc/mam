package comfyui_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// refs is a request with one input image for each (role, strength) pair.
func refs(prompt string, opts ...localcfg.RefOption) modeladapter.ImageRequest {
	req := modeladapter.ImageRequest{Prompt: prompt, AspectRatio: "1:1", Options: localcfg.ImageOptions{Refs: opts}}
	for range opts {
		req.Inputs = append(req.Inputs, modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"})
	}
	return req
}

func ref(role string, strength float64) localcfg.RefOption {
	return localcfg.RefOption{Role: role, Strength: strength}
}

func linkTo(raw json.RawMessage) string {
	var a []json.RawMessage
	_ = json.Unmarshal(raw, &a)
	if len(a) != 2 {
		return ""
	}
	return string(a[0]) + "/" + string(a[1])
}

// A run without a reference image samples from an empty latent and leaves the reference nodes out, so that the same
// workflow is the text-to-image one.
func TestImg2ImgWorkflowWithoutAReferenceIsTextToImage(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-chroma", nil)
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x", AspectRatio: "1:1"})
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, `"737"/0`, linkTo(g["747"].Inputs["latent_image"]))
	assert.Equal(t, `"751"/0`, linkTo(g["747"].Inputs["sigmas"]))
	for _, id := range []string{"760", "761", "762"} {
		_, found := g[id]
		assert.False(t, found, "node %s is only for a reference", id)
	}
	assert.Equal(t, 0, f.Count("POST /upload/image"))
}

// The strength of the reference is the share of the picture it keeps: the sampler's denoise falls from 1 (strength 0, the
// reference is ignored) to the workflow's floor (strength 1). Chroma's floor is 0.6 because its picture does not change below
// that; a workflow without a floor goes down to the least denoise.
func TestImg2ImgStrengthSetsTheDenoiseBetweenOneAndTheFloor(t *testing.T) {
	cases := []struct {
		template string
		node     string
		strength float64
		denoise  float64
	}{
		{"txt2img-chroma", "763", 0, 0.72},  // 0 is "not given": the default 0.7
		{"txt2img-chroma", "763", 0.5, 0.8}, //
		{"txt2img-chroma", "763", 1, 0.6},   // the floor
		{"txt2img-flux-dev-lora", "3", 0.9, 0.1},
		{"txt2img-flux-dev-lora", "3", 1, 0.02}, // no floor: the least denoise
	}
	for _, c := range cases {
		f := comfyuitest.New(t)
		b := backend(t, f, c.template, nil)
		_, err := b.GenerateImage(ctx(t), "", refs("x", ref("", c.strength)))
		require.NoError(t, err)
		g := f.Graph()
		assert.InDelta(t, c.denoise, num(g[c.node].Inputs["denoise"]), 1e-9, "%s at strength %v", c.template, c.strength)
	}
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-chroma", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("x", ref("", 0.7)))
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, `"762"/0`, linkTo(g["747"].Inputs["latent_image"]))
	assert.Equal(t, `"763"/1`, linkTo(g["747"].Inputs["sigmas"]))
	assert.Equal(t, 1024.0, num(g["761"].Inputs["width"]), "the reference is scaled to the size of the result")
}

// A LoRA of the request goes behind the model node: whoever took the model now takes the LoRA's output.
func TestLoRAIsInsertedBehindTheModelNode(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-flux-dev-lora", nil)
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x", Options: localcfg.ImageOptions{LoRA: &localcfg.LoRA{Name: "face.safetensors", Strength: 0.4}}})
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, "face.safetensors", str(g["mam_lora"].Inputs["lora_name"]))
	assert.InDelta(t, 0.4, num(g["mam_lora"].Inputs["strength_model"]), 1e-9)
	assert.Equal(t, `"12"/0`, linkTo(g["mam_lora"].Inputs["model"]), "behind the LoRA the template already has")
	assert.Equal(t, `"mam_lora"/0`, linkTo(g["3"].Inputs["model"]))
}

// An editing workflow takes any number of references up to its slots: the slots of the missing ones are left out.
func TestEditWorkflowKeepsTheSlotsOfTheReferencesOnly(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("<image2> in the pose of <image1>", ref("edit", 1), ref("", 1)))
	require.NoError(t, err)
	g := f.Graph()
	enc := g["6"].Inputs
	assert.Equal(t, `"101"/0`, linkTo(enc["images.image_1"]))
	assert.Equal(t, `"102"/0`, linkTo(enc["images.image_2"]))
	assert.Nil(t, enc["images.image_3"])
	_, found := g["103"]
	assert.False(t, found)
	assert.Len(t, f.Uploads(), 2)
	assert.Equal(t, "<image2> in the pose of <image1>", str(enc["prompt"]), "an edit and a whole picture need no sentence")
	assert.NotNil(t, enc["vae"], "reference latents for the pictures that are reproduced")
}

// A face reference is read by the vision tower alone and the prompt says what to take from it.
func TestFaceReferenceIsToldToTheEncoderAndHasNoLatents(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("a woman on a beach", ref("face", 1)))
	require.NoError(t, err)
	enc := f.Graph()["6"].Inputs
	assert.Contains(t, str(enc["prompt"]), "Keep only the face and identity of <image1>")
	assert.Contains(t, str(enc["prompt"]), "a woman on a beach")
	assert.Nil(t, enc["vae"])
}

// A weaker reference leaves the sampling earlier: the sampling is cut at the step where it leaves, and the part after that is
// encoded without it, with the <imageN> of the prompt renumbered.
func TestWeakerReferenceLeavesTheSamplingEarlier(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", nil)
	// 25 steps: strength 0.6 keeps the reference for 15 of them; strength 1 for all
	_, err := b.GenerateImage(ctx(t), "", refs("<image1> next to <image2>", ref("edit", 0.6), ref("edit", 1)))
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, 15.0, num(g["mam_split_0"].Inputs["step"]))
	assert.Equal(t, `"101"/0`, linkTo(g["mam_enc_0"].Inputs["images.image_1"]))
	assert.Equal(t, `"102"/0`, linkTo(g["mam_enc_0"].Inputs["images.image_2"]))
	assert.Equal(t, "<image1> next to <image2>", str(g["mam_enc_0"].Inputs["prompt"]))
	assert.Equal(t, `"102"/0`, linkTo(g["mam_enc_1"].Inputs["images.image_1"]), "the second reference is the first one left")
	assert.Nil(t, g["mam_enc_1"].Inputs["images.image_2"])
	assert.Equal(t, "the reference image next to <image1>", str(g["mam_enc_1"].Inputs["prompt"]))
	_, ks := g["3"]
	assert.False(t, ks, "the KSampler is replaced by the staged sampling")
	assert.Equal(t, `"mam_run_1"/0`, linkTo(g["8"].Inputs["samples"]))
	assert.Equal(t, `"mam_run_0"/0`, linkTo(g["mam_run_1"].Inputs["latent_image"]))
	assert.Equal(t, `"5"/0`, linkTo(g["mam_run_0"].Inputs["latent_image"]))
}

// The request's seed reaches the staged sampling.
func TestStagedSamplingKeepsTheSeed(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", nil)
	seed := int64(77)
	_, err := b.GenerateImage(ctx(t), "", withSeed(refs("x", ref("face", 0.5)), &seed))
	require.NoError(t, err)
	assert.Equal(t, 77.0, num(f.Graph()["mam_noise"].Inputs["noise_seed"]))
}

// A weak reference is in the first steps only: at strength 0.3 of 25 steps that is 8, and the rest of the run has no image at
// all (it once stayed in because the cut was compared as a fraction of the steps, after rounding).
func TestVeryWeakReferenceIsGoneAfterItsLastStep(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("a woman on a beach", ref("face", 0.3)))
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, 8.0, num(g["mam_split_0"].Inputs["step"]))
	assert.NotNil(t, g["mam_enc_0"].Inputs["images.image_1"])
	for name := range g["mam_enc_1"].Inputs {
		assert.NotContains(t, name, "images.image_", "the second part is encoded without any reference")
	}
	assert.Equal(t, "a woman on a beach", str(g["mam_enc_1"].Inputs["prompt"]), "and without the sentence about it")
}

func withSeed(req modeladapter.ImageRequest, seed *int64) modeladapter.ImageRequest {
	req.Seed = seed
	return req
}
