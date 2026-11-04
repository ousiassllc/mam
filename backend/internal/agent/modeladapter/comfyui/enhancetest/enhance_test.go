// Package enhancetest pins how a video enhancement workflow (Issue #1031, localcfg/videoenhance) is run on the ComfyUI server: the source video is
// uploaded under the extension of its container, and the values of the operation reach the graph.
package enhancetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/enhancevideo"
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

func str(raw json.RawMessage) string  { var s string; _ = json.Unmarshal(raw, &s); return s }
func num(raw json.RawMessage) float64 { var n float64; _ = json.Unmarshal(raw, &n); return n }

// filepath reads the staged file of a Blob (and discards it).
func filepath(t *testing.T, b modeladapter.Blob) []byte {
	t.Helper()
	data, err := b.Bytes()
	require.NoError(t, err)
	b.Discard()
	return data
}

var sourceMP4 = modeladapter.Input{Data: []byte("\x00\x00\x00\x18ftypisom-source-video"), MimeType: "video/mp4"}

func TestEnhanceVideoUpscaleUploadsTheSourceAsMP4AndPutsTheScaleIntoTheGraph(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputBody = []byte("enhanced-mp4")
	b := backend(t, f, "video-upscale-seedvr2-3b")

	res, err := b.EnhanceVideo(ctx(t), enhancevideo.Request{Source: sourceMP4, Scale: 2})
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.Equal(t, []byte("enhanced-mp4"), filepath(t, res.File))
	require.NotNil(t, res.ActualCost)
	assert.Zero(t, *res.ActualCost, "a local run has no bill")

	assert.Equal(t, 1, f.Count("POST /upload/image"), "the source video is uploaded once")
	require.Len(t, f.Uploads(), 1)
	assert.Regexp(t, `\.mp4$`, f.Uploads()[0], "the extension tells the server it is a video")
	assert.Equal(t, sourceMP4.Data, f.UploadBodies()[0])

	g := f.Graph()
	assert.Equal(t, f.Uploads()[0], str(g["1"].Inputs["file"]), "LoadVideo takes the uploaded file")
	assert.Equal(t, 2.0, num(g["3"].Inputs["resize_type.multiplier"]))
	assert.Equal(t, "SaveVideo", g["15"].ClassType)
}

func TestEnhanceVideoInterpolatePutsTheMultiplierAndTheFrameRateIntoTheGraph(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "video-interpolate-rife-v4.25")

	res, err := b.EnhanceVideo(ctx(t), enhancevideo.Request{
		Source: modeladapter.Input{Data: []byte("webm-bytes"), MimeType: "video/webm"}, Multiplier: 4, OutputFPS: 64,
	})
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", res.MimeType)
	filepath(t, res.File)

	require.Len(t, f.Uploads(), 1)
	assert.Regexp(t, `\.webm$`, f.Uploads()[0])
	g := f.Graph()
	assert.Equal(t, f.Uploads()[0], str(g["1"].Inputs["file"]))
	assert.Equal(t, 4.0, num(g["4"].Inputs["multiplier"]))
	assert.Equal(t, 64.0, num(g["5"].Inputs["fps"]))
	assert.Equal(t, "rife_v4.25.safetensors", str(g["3"].Inputs["model_name"]))
}

func TestEnhanceVideoRefusesWhatTheWorkflowCannotRunBeforeUploadingAnything(t *testing.T) {
	cases := map[string]struct {
		tpl string
		req enhancevideo.Request
	}{
		"empty source":       {"video-upscale-seedvr2-3b", enhancevideo.Request{Scale: 2}},
		"scale of 1":         {"video-upscale-seedvr2-3b", enhancevideo.Request{Source: sourceMP4, Scale: 1}},
		"no multiplier":      {"video-interpolate-rife-v4.26", enhancevideo.Request{Source: sourceMP4, OutputFPS: 32}},
		"no output fps":      {"video-interpolate-rife-v4.26", enhancevideo.Request{Source: sourceMP4, Multiplier: 2}},
		"multiplier over 16": {"video-interpolate-rife-v4.26", enhancevideo.Request{Source: sourceMP4, Multiplier: 17, OutputFPS: 32}},
	}
	for name, c := range cases {
		f := comfyuitest.New(t)
		b := backend(t, f, c.tpl)
		_, err := b.EnhanceVideo(ctx(t), c.req)
		require.Error(t, err, name)
		assert.Zero(t, f.Count("POST /upload/image"), name)
		assert.Zero(t, f.Count("POST /prompt"), name)
	}
}

func TestEnhanceVideoRefusesAWorkflowThatIsNoEnhancement(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "img2video-wan22-5b")
	_, err := b.EnhanceVideo(ctx(t), enhancevideo.Request{Source: sourceMP4, Scale: 2})
	require.Error(t, err)
	assert.True(t, errors.Is(err, enhancevideo.ErrNotEnhance))
	assert.Zero(t, f.Count("POST /upload/image"))
}

func TestEnhanceVideoOutputThatIsNotAVideoIsAnErrorAfterSubmit(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputFile = "out.png"
	b := backend(t, f, "video-upscale-seedvr2-3b")
	_, err := b.EnhanceVideo(ctx(t), enhancevideo.Request{Source: sourceMP4, Scale: 2})
	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrAfterSubmit, "the job ran on the GPU: it is not run again")
}
