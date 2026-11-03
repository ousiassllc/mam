// Package controlnettest tests the workflows that run on one reference image through a ControlNet (core nodes) or an IP-Adapter (the node
// ipadapter-plus): the image is the control image, the strength of the reference the strength of the node (Issue #976). It is a package of
// its own because comfyui is at its size limit.
package controlnettest

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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
	tp, ok := localcfg.TemplateByID(tpl)
	require.True(t, ok)
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: tp.Model}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

// photo is a readable picture with one contour (a dark disc on gray): a ControlNet workflow makes its edge map from the reference (Issue #993).
func photo(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 96, 96))
	for y := range 96 {
		for x := range 96 {
			v := uint8(170)
			if (x-48)*(x-48)+(y-48)*(y-48) <= 30*30 {
				v = 40
			}
			img.SetGray(x, y, color.Gray{v})
		}
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func refs(t *testing.T, prompt string, strength float64) modeladapter.ImageRequest {
	return refsOf(prompt, strength, photo(t))
}

func refsOf(prompt string, strength float64, data []byte) modeladapter.ImageRequest {
	return modeladapter.ImageRequest{
		Prompt: prompt, AspectRatio: "1:1", Options: localcfg.ImageOptions{Refs: []localcfg.RefOption{{Strength: strength}}},
		Inputs: []modeladapter.Input{{Data: data, MimeType: "image/png"}},
	}
}

func str(raw json.RawMessage) string  { var s string; _ = json.Unmarshal(raw, &s); return s }
func num(raw json.RawMessage) float64 { var n float64; _ = json.Unmarshal(raw, &n); return n }

func linkTo(raw json.RawMessage) string {
	var a []json.RawMessage
	_ = json.Unmarshal(raw, &a)
	if len(a) != 2 {
		return ""
	}
	return string(a[0]) + "/" + string(a[1])
}

// A ControlNet workflow (Issue #976) takes the reference image as its control image and the strength of the reference as the
// ControlNet's strength: the picture is uploaded, the LoadImage node names it, ControlNetApplyAdvanced gets the strength, and the sampler
// reads the conditioning that comes out of the ControlNet, not the one of the text encoders.
func TestControlNetWorkflowTakesTheReferenceAsTheControlImageAndItsStrength(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl-controlnet")

	_, err := b.GenerateImage(ctx(t), "", refs(t, "a cat", 0.35))
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
		b := backend(t, f, "txt2img-sdxl-controlnet")
		_, err := b.GenerateImage(ctx(t), "", refs(t, "x", strength))
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
		b := backend(t, f, template)
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
	b := backend(t, f, "txt2img-sdxl-ipadapter")
	_, err := b.GenerateImage(ctx(t), "", refs(t, "a cat", 0.5))
	require.NoError(t, err)
	g := f.Graph()
	assert.InDelta(t, 0.5, num(g["13"].Inputs["weight"]), 1e-9)
	assert.Equal(t, `"10"/0`, linkTo(g["13"].Inputs["image"]))
	assert.Equal(t, `"13"/0`, linkTo(g["3"].Inputs["model"]))
	assert.Equal(t, 1, f.Count("POST /upload/image"))
}

// #993: the ControlNet workflow is given a photograph and uploads its Canny edge map, drawn by mam (no preprocessor node, no Python package).
func TestControlNetWorkflowUploadsTheEdgeMapOfThePhotograph(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl-controlnet")
	_, err := b.GenerateImage(ctx(t), "", refs(t, "a cat", 0.7))
	require.NoError(t, err)

	require.Len(t, f.UploadBodies(), 1)
	assert.NotEqual(t, photo(t), f.UploadBodies()[0], "the photograph itself is not the control image")
	edges, err := png.Decode(bytes.NewReader(f.UploadBodies()[0]))
	require.NoError(t, err)
	onRing := func(x, y int) bool { r, _, _, _ := edges.At(x, y).RGBA(); return r > 0x8000 }
	assert.True(t, onRing(48+30, 48) || onRing(48+29, 48) || onRing(48+31, 48), "the contour of the disc is a white line")
	assert.False(t, onRing(48, 48), "the inside of the disc is black")
	assert.False(t, onRing(2, 2), "so is the flat background")
	assert.Regexp(t, `\.png$`, f.Uploads()[0])
}

// A user who prepared the edge map gives it as before: the same bytes are uploaded.
func TestControlNetWorkflowUploadsAnEdgeMapTheUserMadeAsItIs(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for x := 10; x < 54; x++ {
		img.SetGray(x, 20, color.Gray{255})
	}
	var edgeMap bytes.Buffer
	require.NoError(t, png.Encode(&edgeMap, img))

	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl-controlnet")
	_, err := b.GenerateImage(ctx(t), "", refsOf("x", 0.7, edgeMap.Bytes()))
	require.NoError(t, err)
	assert.Equal(t, [][]byte{edgeMap.Bytes()}, f.UploadBodies())
}

func TestControlNetWorkflowRefusesAReferenceThatIsNotAPicture(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl-controlnet")
	_, err := b.GenerateImage(ctx(t), "", refsOf("x", 0.7, []byte("not a picture")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "control image")
	assert.Zero(t, f.Count("POST /prompt"))
}

// The IP-Adapter takes the picture as it is: no preprocessing.
func TestIPAdapterWorkflowUploadsThePictureAsItIs(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl-ipadapter")
	_, err := b.GenerateImage(ctx(t), "", refs(t, "x", 0.5))
	require.NoError(t, err)
	assert.Equal(t, [][]byte{photo(t)}, f.UploadBodies())
}

// A depth or normal workflow (Issue #1020) is given a depth or a normal map (here the photograph stands for it) and uploads it as it is: it
// is the control image already, and mam draws no edges from it. The normal one reads the ControlNet whose type was set to normal.
func TestDepthAndNormalControlNetWorkflowsUploadTheMapAsItIs(t *testing.T) {
	for template, control := range map[string]string{"txt2img-sdxl-controlnet-depth": `"12"/0`, "txt2img-sdxl-controlnet-normal": `"14"/0`} {
		f := comfyuitest.New(t)
		b := backend(t, f, template)
		req := refs(t, "a cat", 0.6)
		req.Options.Refs[0].Role = localcfg.RolePose // the pose library's map is given as the pose
		_, err := b.GenerateImage(ctx(t), "", req)
		require.NoError(t, err, template)

		assert.Equal(t, [][]byte{photo(t)}, f.UploadBodies(), template+": the map itself is the control image")
		g := f.Graph()
		assert.InDelta(t, 0.6, num(g["13"].Inputs["strength"]), 1e-9, template)
		assert.Equal(t, `"10"/0`, linkTo(g["13"].Inputs["image"]), template)
		assert.Equal(t, control, linkTo(g["13"].Inputs["control_net"]), template)
		assert.Equal(t, `"13"/0`, linkTo(g["3"].Inputs["positive"]), template)
	}
}
