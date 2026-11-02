// Package editplustest tests the Qwen-Image-Edit 2511 workflow (Issue #971) through the ComfyUI backend against the fake server. It is a
// package of its own because the directory of the backend's tests is full (linterly).
package editplustest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// submitted is the graph the fake server got (comfyuitest.Fake.Graph).
type submitted = map[string]struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
}

func backend(t *testing.T, f *comfyuitest.Fake, tpl string) *comfyui.Backend {
	t.Helper()
	tpl2, ok := localcfg.TemplateByID(tpl)
	require.True(t, ok)
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: tpl2.Model}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func str(raw json.RawMessage) string  { var s string; _ = json.Unmarshal(raw, &s); return s }
func num(raw json.RawMessage) float64 { var n float64; _ = json.Unmarshal(raw, &n); return n }

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

// Qwen-Image-Edit 2511 (Issue #971) takes its references through TextEncodeQwenImageEditPlus (image1..image3), once for the
// positive and once for the negative conditioning. The staging has to keep both encoders in step.

// slotsOf are the reference images (image1..3) an EditPlus encoder node holds.
func slotsOf(g submitted, id string) (n int) {
	for name := range g[id].Inputs {
		if strings.HasPrefix(name, "image") {
			n++
		}
	}
	return n
}

func TestEditPlusRunKeepsTheReferencesOfBothEncoders(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image-edit-2511")
	_, err := b.GenerateImage(ctx(t), "", refs("keep the face of <image1>, take the mood of <image2>", ref("face", 1), ref("style", 1), ref("edit", 1)))
	require.NoError(t, err)
	g := f.Graph()

	assert.Equal(t, 3, f.Count("POST /upload/image"))
	for _, id := range []string{"6", "7"} {
		assert.Equal(t, 3, slotsOf(g, id), "encoder %s has the three references", id)
	}
	prompt := str(g["6"].Inputs["prompt"])
	assert.Contains(t, prompt, "Keep only the face and identity of Picture 1", "the role sentence names the picture the EditPlus encoder numbers")
	assert.Contains(t, prompt, "take the mood of Picture 2", "the <imageN> of the user's prompt is the encoder's Picture N")
	assert.NotContains(t, prompt, "<image", "no tag is left for the encoder to read as plain text")
	neg := str(g["7"].Inputs["prompt"])
	assert.NotEmpty(t, neg, "the negative encoder keeps the negative prompt (mam's default)")
	assert.NotContains(t, neg, "Keep only the face", "the role sentences are for the positive prompt only")
	assert.Equal(t, `"39"/0`, linkTo(g["6"].Inputs["vae"]), "the picture to edit is also given as a latent")
	assert.Equal(t, `"39"/0`, linkTo(g["7"].Inputs["vae"]))
	assert.Equal(t, `"6"/0`, linkTo(g["3"].Inputs["positive"]))
	assert.Equal(t, `"7"/0`, linkTo(g["3"].Inputs["negative"]))
}

// A face or a style is read by the vision tower alone: without the VAE the encoder makes no latent of the picture.
func TestEditPlusFaceAndStyleReferencesGetNoLatent(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image-edit-2511")
	_, err := b.GenerateImage(ctx(t), "", refs("x", ref("face", 1), ref("style", 1)))
	require.NoError(t, err)
	g := f.Graph()
	for _, id := range []string{"6", "7"} {
		assert.Equal(t, 2, slotsOf(g, id), id)
		assert.Nil(t, g[id].Inputs["vae"], id)
	}
	_, third := g["103"]
	assert.False(t, third, "the slot of the missing third reference is left out")
}

// A run without a reference is text-to-image: no LoadImage node, no image on either encoder, and nothing is uploaded.
func TestEditPlusWithoutReferencesIsTextToImage(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image-edit-2511")
	_, err := b.GenerateImage(ctx(t), "", refs("a cat"))
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, 0, f.Count("POST /upload/image"))
	assert.Empty(t, idsOfClass(g, "LoadImage"))
	for _, id := range []string{"6", "7"} {
		assert.Zero(t, slotsOf(g, id), id)
	}
	assert.Equal(t, "a cat", str(g["6"].Inputs["prompt"]))
}

func idsOfClass(g submitted, class string) (ids []string) {
	for id, n := range g {
		if n.ClassType == class {
			ids = append(ids, id)
		}
	}
	return ids
}

// A reference of a lower strength leaves the conditioning part way: each part is encoded again for the positive and for the
// negative conditioning with the references still in it, and the guider of a part takes both.
func TestEditPlusStrengthCutsTheSamplingAndEncodesBothSides(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image-edit-2511")
	_, err := b.GenerateImage(ctx(t), "", refs("x", ref("edit", 1), ref("style", 0.5)))
	require.NoError(t, err)
	g := f.Graph()

	for _, gone := range []string{"3", "6", "7"} {
		_, found := g[gone]
		assert.False(t, found, "node %s is replaced by the parts", gone)
	}
	assert.Equal(t, 10.0, num(g["mam_split_0"].Inputs["step"]), "0.5 of the 20 steps")
	assert.Equal(t, []int{2, 1}, []int{slotsOf(g, "mam_enc_0"), slotsOf(g, "mam_enc_1")}, "the style reference is gone from the second part")
	assert.Equal(t, []int{2, 1}, []int{slotsOf(g, "mam_negenc_0"), slotsOf(g, "mam_negenc_1")}, "the negative encoder follows")
	for k := range 2 {
		guider := g[fmt.Sprintf("mam_guider_%d", k)]
		assert.Equal(t, fmt.Sprintf(`"mam_enc_%d"/0`, k), linkTo(guider.Inputs["positive"]))
		assert.Equal(t, fmt.Sprintf(`"mam_negenc_%d"/0`, k), linkTo(guider.Inputs["negative"]))
		assert.Equal(t, `"75"/0`, linkTo(guider.Inputs["model"]), "the model still goes through CFGNorm")
		assert.Equal(t, 4.0, num(guider.Inputs["cfg"]))
	}
	assert.Contains(t, str(g["mam_enc_0"].Inputs["prompt"]), "Picture 2", "the style sentence is in the first part")
	assert.NotContains(t, str(g["mam_enc_1"].Inputs["prompt"]), "art style", "and gone from the second")
}

// A LoRA of the request goes behind the diffusion model, in front of ModelSamplingAuraFlow and CFGNorm.
func TestEditPlusLoRAGoesBehindTheDiffusionModel(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-qwen-image-edit-2511")
	req := refs("x", ref("edit", 1))
	req.Options.LoRA = &localcfg.LoRA{Name: "lightning.safetensors", Strength: 1}
	_, err := b.GenerateImage(ctx(t), "", req)
	require.NoError(t, err)
	g := f.Graph()
	assert.Equal(t, `"37"/0`, linkTo(g["mam_lora"].Inputs["model"]))
	assert.Equal(t, `"mam_lora"/0`, linkTo(g["66"].Inputs["model"]))
}
