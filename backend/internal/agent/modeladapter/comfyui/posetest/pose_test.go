// Package posetest tests the Shisei pose-reference path of the graphs (Issue #986): a pose chart is given to the Qwen-Image 2.1
// workflow (TextEncodeQwenImage21) and to Qwen-Image-Edit 2511 (TextEncodeQwenImageEditPlus) next to the character's picture, with the
// role "pose": the encoder numbers it, the prompt says "only the pose", and the chart is looked at by the vision tower without a latent.
// It is a package of its own because the directory of the backend's tests is full (linterly).
package posetest_test

import (
	"context"
	"encoding/json"
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

// submitted is the graph the fake server got (comfyuitest.Fake.Graph).
type submitted = map[string]struct {
	ClassType string                     `json:"class_type"`
	Inputs    map[string]json.RawMessage `json:"inputs"`
}

func str(raw json.RawMessage) string { var s string; _ = json.Unmarshal(raw, &s); return s }

// refs is a request with one input image for each (role, strength) pair.
func refs(prompt string, opts ...localcfg.RefOption) modeladapter.ImageRequest {
	req := modeladapter.ImageRequest{Prompt: prompt, AspectRatio: "1:1", Options: localcfg.ImageOptions{Refs: opts}}
	for range opts {
		req.Inputs = append(req.Inputs, modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"})
	}
	return req
}

func ref(role string) localcfg.RefOption { return localcfg.RefOption{Role: role, Strength: 1} }

// slots are the names of the reference images an encoder node holds: "images.image_N" (TextEncodeQwenImage21), "imageN" (EditPlus).
func slots(g submitted, id string) (n int) {
	for name := range g[id].Inputs {
		if strings.HasPrefix(name, "image") {
			n++
		}
	}
	return n
}

const poseSentence = "only as a pose reference"

// Both workflows take a pose chart: the character is picture 1 (the whole picture), the chart picture 2 (the pose only).
func TestAPoseChartGoesToTheEncoderNextToTheCharacter(t *testing.T) {
	for _, tc := range []struct {
		name, template, class string
	}{
		{"Qwen-Image 2.1", "txt2img-qwen-image21", "TextEncodeQwenImage21"},
		{"Qwen-Image-Edit 2511", "txt2img-qwen-image-edit-2511", "TextEncodeQwenImageEditPlus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := comfyuitest.New(t)
			b := backend(t, f, tc.template)
			_, err := b.GenerateImage(ctx(t), "", refs("the girl of <image1> in the pose of <image2>", ref("reference"), ref("pose")))
			require.NoError(t, err)
			g := f.Graph()

			assert.Equal(t, 2, f.Count("POST /upload/image"), "the character and the chart are uploaded")
			assert.Equal(t, tc.class, g["6"].ClassType)
			assert.Equal(t, 2, slots(g, "6"), "two reference images on the encoder")
			prompt := str(g["6"].Inputs["prompt"])
			assert.Contains(t, prompt, poseSentence)
			assert.Contains(t, prompt, "copy only the body pose and the camera angle")
			assert.NotContains(t, prompt, "Keep only the face", "the character has no sentence of its own: it is the whole picture")
			assert.NotEmpty(t, g["6"].Inputs["vae"], "the character is a whole picture: the encoder makes its latent")
		})
	}
}

// A pose chart alone (with a face and a style) is looked at by the vision tower only: no VAE, no latent of a chart that is not to
// appear in the picture.
func TestAPoseChartAloneGetsNoLatent(t *testing.T) {
	for _, tpl := range []string{"txt2img-qwen-image21", "txt2img-qwen-image-edit-2511"} {
		t.Run(tpl, func(t *testing.T) {
			f := comfyuitest.New(t)
			b := backend(t, f, tpl)
			_, err := b.GenerateImage(ctx(t), "", refs("x", ref("face"), ref("pose")))
			require.NoError(t, err)
			g := f.Graph()
			assert.Nil(t, g["6"].Inputs["vae"], "face and pose are not reproduced")
			assert.Equal(t, 2, slots(g, "6"))
			assert.Contains(t, str(g["6"].Inputs["prompt"]), poseSentence)
		})
	}
}

// The role sentence of the pose says "Picture N" for the EditPlus encoder (which numbers its images) and keeps the tag for the 2.1 encoder.
func TestThePoseSentenceNamesThePictureTheEncoderNumbers(t *testing.T) {
	f := comfyuitest.New(t)
	_, err := backend(t, f, "txt2img-qwen-image-edit-2511").GenerateImage(ctx(t), "", refs("x", ref("reference"), ref("pose")))
	require.NoError(t, err)
	assert.Contains(t, str(f.Graph()["6"].Inputs["prompt"]), "Picture 2 only as a pose reference")
	assert.NotContains(t, str(f.Graph()["6"].Inputs["prompt"]), "<image")

	f2 := comfyuitest.New(t)
	_, err = backend(t, f2, "txt2img-qwen-image21").GenerateImage(ctx(t), "", refs("x", ref("reference"), ref("pose")))
	require.NoError(t, err)
	assert.Contains(t, str(f2.Graph()["6"].Inputs["prompt"]), "<image2> only as a pose reference")
}
