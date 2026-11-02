package comfyui_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// More distinct strengths than the staging has parts for: the cuts kept are spread over the strengths, and a reference whose
// own cut was dropped moves to the nearest kept one. None of them may stay to the end of the run (it once did: the cuts were
// truncated to the weakest ones and the stronger references were in every part).
func TestManyDistinctStrengthsSnapToTheKeptCuts(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", nil)
	// 25 steps: the strengths 0.1..0.8 are the steps 3 5 8 10 13 15 18 20
	var opts []localcfg.RefOption
	for s := 1; s <= 8; s++ {
		opts = append(opts, ref("edit", float64(s)/10))
	}
	_, err := b.GenerateImage(ctx(t), "", refs("x", opts...))
	require.NoError(t, err)
	g := f.Graph()

	var cuts []float64
	for k := range 5 {
		cuts = append(cuts, num(g[fmt.Sprintf("mam_split_%d", k)].Inputs["step"]))
	}
	// the splits are lengths of the parts: 3, 5, 5, 2, 5 from the kept cuts 3 8 13 15 20
	assert.Equal(t, []float64{3, 5, 5, 2, 5}, cuts)
	_, more := g["mam_split_5"]
	assert.False(t, more, "at most maxStages parts")

	imagesOf := func(id string) (n int) {
		for name := range g[id].Inputs {
			if len(name) > len("images.image_") && name[:len("images.image_")] == "images.image_" {
				n++
			}
		}
		return n
	}
	// the references still in each part: 8 up to step 3, then those that leave later (5->3, 10->8, 18->20 are snapped)
	assert.Equal(t, []int{8, 6, 4, 3, 2, 0}, []int{imagesOf("mam_enc_0"), imagesOf("mam_enc_1"), imagesOf("mam_enc_2"), imagesOf("mam_enc_3"), imagesOf("mam_enc_4"), imagesOf("mam_enc_5")})
}

// The VAE the encoder gets is the one the VAEDecode node takes, whatever other VAELoader the graph has and however the map
// of nodes is walked.
func TestEncoderGetsTheVAEOfTheDecoder(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", func(m *localcfg.Model) {
		var g map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(m.Workflow.Graph, &g))
		g["1"] = json.RawMessage(`{"class_type": "VAELoader", "inputs": {"vae_name": "other.safetensors"}}`)
		g["2"] = json.RawMessage(`{"class_type": "VAELoader", "inputs": {"vae_name": "another.safetensors"}}`)
		m.Workflow.Graph, _ = json.Marshal(g)
	})
	for range 12 {
		_, err := b.GenerateImage(ctx(t), "", refs("x", ref("edit", 1)))
		require.NoError(t, err)
		assert.Equal(t, `"39"/0`, linkTo(f.Graph()["6"].Inputs["vae"]))
	}
}

// An empty prompt leaves no empty line behind the sentences about the references.
func TestEmptyPromptLeavesNoTrailingNewline(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image21", nil)
	_, err := b.GenerateImage(ctx(t), "", refs("", ref("face", 1)))
	require.NoError(t, err)
	got := str(f.Graph()["6"].Inputs["prompt"])
	assert.NotEmpty(t, got)
	assert.NotContains(t, got[len(got)-1:], "\n")
}

// Inputs without options (a character sheet, a workflow step) are no references for a bundled model: it runs text-to-image,
// uploads nothing and keeps no reference node. A workflow without a reference mode still takes them.
func TestBareInputsAreNoReferencesForAModelWithAReferenceMode(t *testing.T) {
	bare := func(prompt string) modeladapter.ImageRequest {
		return modeladapter.ImageRequest{Prompt: prompt, AspectRatio: "1:1", Inputs: []modeladapter.Input{{Data: comfyuitest.PNG, MimeType: "image/png"}}}
	}
	t.Run("img2img", func(t *testing.T) {
		f := comfyuitest.New(t)
		b := backend(t, f, "txt2img-chroma", nil)
		_, err := b.GenerateImage(ctx(t), "", bare("x"))
		require.NoError(t, err)
		g := f.Graph()
		assert.Equal(t, `"737"/0`, linkTo(g["747"].Inputs["latent_image"]), "sampled from an empty latent")
		_, found := g["760"]
		assert.False(t, found)
		assert.Equal(t, 0, f.Count("POST /upload/image"))
	})
	t.Run("edit", func(t *testing.T) {
		f := comfyuitest.New(t)
		b := backend(t, f, "txt2img-qwen-image21", nil)
		_, err := b.GenerateImage(ctx(t), "", bare("x"))
		require.NoError(t, err)
		g := f.Graph()
		_, found := g["101"]
		assert.False(t, found)
		assert.Nil(t, g["6"].Inputs["images.image_1"])
		assert.Equal(t, 0, f.Count("POST /upload/image"))
	})
	t.Run("no reference mode", func(t *testing.T) {
		f := comfyuitest.New(t)
		b := backend(t, f, "img2img-sdxl", nil)
		_, err := b.GenerateImage(ctx(t), "", bare("x"))
		require.NoError(t, err)
		assert.Equal(t, 1, f.Count("POST /upload/image"))
	})
}
