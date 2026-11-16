// Package audiolengthtest tests how the length of the sound becomes the length of the clip of a speech-to-video model (Issue #1153): the unit (audiolength), the call
// of direct.Video that uses it, and the graph the ComfyUI backend builds for a short and for a long sound, over the fake server.
package audiolengthtest_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/audiolength"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// wav is a sound of the given number of milliseconds (8 kHz, 16 bit mono: small enough for a minute).
func wav(ms int) *modeladapter.Input {
	return &modeladapter.Input{Data: audiofiletest.WAV(8000, 8*ms), MimeType: "audio/wav"}
}

func s2vCaps(t *testing.T) modeladapter.VideoCapabilities {
	t.Helper()
	tpl, ok := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	require.True(t, ok)
	c, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	return modeladapter.VideoCapabilities(c)
}

func TestTheLengthOfTheClipIsTheSoundsRoundedUpToATenthOfASecond(t *testing.T) {
	caps := s2vCaps(t)
	cfg := modeladapter.DefaultConfig()
	for _, c := range []struct {
		ms   int
		want float64
	}{{3000, 3}, {3040, 3.1}, {3100, 3.1}, {59990, 60}, {60000, 60}, {250, 0.3}} {
		req, err := audiolength.Apply(cfg, caps, modeladapter.VideoRequest{DurationSeconds: 5, Audio: wav(c.ms)})
		require.NoError(t, err, "%d ms", c.ms)
		assert.InDelta(t, c.want, req.DurationSeconds, 1e-9, "%d ms (the request's 5 s is ignored)", c.ms)
	}
}

func TestApplyLeavesAnyOtherRequestAlone(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	plain := s2vCaps(t)
	plain.AudioLength = false
	req := modeladapter.VideoRequest{DurationSeconds: 5, Audio: wav(3000)}
	got, err := audiolength.Apply(cfg, plain, req)
	require.NoError(t, err)
	assert.Equal(t, 5.0, got.DurationSeconds, "a model whose clip has the length of the request")

	got, err = audiolength.Apply(cfg, s2vCaps(t), modeladapter.VideoRequest{DurationSeconds: 5})
	require.NoError(t, err, "no sound: the workflow's own check says it is missing")
	assert.Equal(t, 5.0, got.DurationSeconds)
}

// A sound over gentest.max_audio_seconds, or one whose length cannot be read, is refused (422, input_unsupported) before anything is sent.
func TestASoundThatIsTooLongOrUnreadableIsRefused(t *testing.T) {
	caps := s2vCaps(t)
	cfg := modeladapter.DefaultConfig()
	_, err := audiolength.Apply(cfg, caps, modeladapter.VideoRequest{Audio: wav(61000)})
	ae, ok := apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.CodeInputUnsupported, ae.Code)
	assert.Equal(t, http.StatusUnprocessableEntity, ae.Status())
	assert.Equal(t, "音声が長すぎます（61.0 秒。上限は 60 秒）", ae.Cause)

	limit := 120
	cfg.Gentest.MaxAudioSeconds = &limit
	_, err = audiolength.Apply(cfg, caps, modeladapter.VideoRequest{Audio: wav(61000)})
	assert.NoError(t, err, "the limit is the setting")
	limit = 10
	_, err = audiolength.Apply(cfg, caps, modeladapter.VideoRequest{Audio: wav(10001)})
	require.Error(t, err)
	_, err = audiolength.Apply(cfg, caps, modeladapter.VideoRequest{Audio: wav(10000)})
	assert.NoError(t, err, "the limit itself is allowed")

	_, err = audiolength.Apply(cfg, caps, modeladapter.VideoRequest{Audio: &modeladapter.Input{Data: []byte("not a sound"), MimeType: "audio/wav"}})
	ae, ok = apperr.As(err)
	require.True(t, ok)
	assert.Equal(t, apperr.CodeInputUnsupported, ae.Code)
	assert.Equal(t, "音声の長さを読めません", ae.Cause)
}

// fakeVideo records the requests it is sent.
type fakeVideo struct {
	mu   sync.Mutex
	reqs []modeladapter.VideoRequest
}

func (f *fakeVideo) Configured() bool { return true }

func (f *fakeVideo) GenerateVideo(_ context.Context, _ string, req modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return modeladapter.VideoResult{File: modeladapter.BlobOf([]byte("mp4")), MimeType: "video/mp4"}, nil
}

// directS2V is direct.Video's adapter with the template as the model and a fake backend behind it.
func directS2V(t *testing.T) (*modeladapter.Adapter, *fakeVideo, modeladapter.VideoTarget) {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"box": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}}
	tpl, _ := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	m := tpl.Model
	m.CostPerSecond = 0.5
	cfg.LocalModels = map[string]localcfg.Model{"box/s2v": m}
	id := domain.ModelName(localcfg.ID(localcfg.KindComfyUI, "box", "s2v"))
	f := &fakeVideo{}
	a := modeladapter.New(cfg, modeladapter.Backends{Video: map[domain.ModelName]modeladapter.VideoBackend{id: f}},
		modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
	return a, f, modeladapter.VideoTarget{Model: id, Resolution: "480p"}
}

func picture() []modeladapter.Input {
	return []modeladapter.Input{{Data: comfyuitest.PNG, MimeType: "image/png"}}
}

// direct.Video is the authority: the length of the request is ignored and replaced by the sound's, which the backend, the output and the cost then use.
func TestDirectVideoTakesTheLengthFromTheSound(t *testing.T) {
	a, f, target := directS2V(t)
	// the request names 5 s, as the form's default does: the sound's length wins
	out, err := direct.Video(t.Context(), a, target, modeladapter.VideoRequest{Prompt: "she sings", DurationSeconds: 5, References: picture(), Audio: wav(3000)})
	require.NoError(t, err)
	out.File.Discard()
	assert.InDelta(t, 3.0, out.DurationSeconds, 1e-9, "the output records the sound's length")
	require.Len(t, f.reqs, 1)
	assert.InDelta(t, 3.0, f.reqs[0].DurationSeconds, 1e-9, "the backend is sent the sound's length")
	assert.InDelta(t, 1.5, out.EstimatedCost, 1e-9)

	out, err = direct.Video(t.Context(), a, target, modeladapter.VideoRequest{Prompt: "p", References: picture(), Audio: wav(42300)})
	require.NoError(t, err, "no length in the request is fine")
	out.File.Discard()
	assert.InDelta(t, 42.3, out.DurationSeconds, 1e-9)
}

// A sound over gentest.max_audio_seconds or unreadable is refused before the backend is called.
func TestDirectVideoRefusesASoundThatIsTooLongOrUnreadable(t *testing.T) {
	a, f, target := directS2V(t)
	for name, sound := range map[string]*modeladapter.Input{
		"too long":   wav(61000),
		"unreadable": {Data: []byte("RIFF\x24\x00\x00\x00WAVEfmt  a short sound"), MimeType: "audio/wav"},
	} {
		_, err := direct.Video(t.Context(), a, target, modeladapter.VideoRequest{DurationSeconds: 5, References: picture(), Audio: sound})
		ae, ok := apperr.As(err)
		require.True(t, ok, name)
		assert.Equal(t, apperr.CodeInputUnsupported, ae.Code, name)
		assert.Equal(t, http.StatusUnprocessableEntity, ae.Status(), name)
	}
	assert.Empty(t, f.reqs)
}

// s2v is the ComfyUI backend of the template over a fake server whose output is the template's own video node.
func s2v(t *testing.T) (*comfyui.Backend, *comfyuitest.Fake) {
	t.Helper()
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "113", []byte("mp4")
	tpl, _ := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: tpl.Model}, f
}

func generate(t *testing.T, seconds float64) *comfyuitest.Fake {
	t.Helper()
	b, f := s2v(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	res, err := b.GenerateVideo(ctx, "", modeladapter.VideoRequest{Prompt: "she sings", DurationSeconds: seconds, Resolution: "480p", AspectRatio: "16:9", References: picture(), Audio: wav(1000)})
	require.NoError(t, err)
	res.File.Discard()
	return f
}

func TestAShortSoundMakesOneChunkTrimmedToItsLength(t *testing.T) {
	g := generate(t, 3).Graph()
	assert.Len(t, g, 16, "the template's nodes: no extension")
	assert.Equal(t, "77", string(g["93"].Inputs["length"]), "the first chunk is a whole chunk")
	assert.Equal(t, "48", string(g["96"].Inputs["length"]), "3 s at 16 fps")
	assert.Equal(t, `["3",0]`, string(g["80"].Inputs["samples"]))
}

func TestALongSoundMakesAChainOfChunksAsLongAsTheSound(t *testing.T) {
	f := generate(t, 60)
	g := f.Graph()
	assert.Len(t, g, 16+12*3, "960 frames need 13 chunks: the first and 12 extensions of 3 nodes")
	assert.Equal(t, "77", string(g["93"].Inputs["length"]))
	assert.Equal(t, "960", string(g["96"].Inputs["length"]))
	assert.Equal(t, `["x12_cat",0]`, string(g["80"].Inputs["samples"]))
	assert.Equal(t, `["x11_cat",0]`, string(g["x12_ext"].Inputs["video_latent"]))
	assert.Equal(t, string(g["3"].Inputs["seed"]), string(g["x12_ks"].Inputs["seed"]), "every chunk is sampled with the seed of the run")
	assert.Equal(t, `"uni_pc"`, string(g["x7_ks"].Inputs["sampler_name"]))
	assert.Equal(t, 1, f.Count("POST /prompt"))
}

// The values the request chose (seed, steps, cfg, sampler, scheduler) are put into the first sampler by the assignments and copied to the samplers of the extension,
// so that every chunk of the clip is sampled as the first.
func TestTheExtensionSamplesWithTheSeedAndTheSamplingOfTheRequest(t *testing.T) {
	b, f := s2v(t)
	steps, cfg, seed := 12, 4.5, int64(777)
	res, err := b.GenerateVideo(t.Context(), "", modeladapter.VideoRequest{
		Prompt: "p", DurationSeconds: 12, Resolution: "480p", AspectRatio: "16:9", References: picture(), Audio: wav(1000), Seed: &seed,
		Sampling: localcfg.SamplingSettings{Steps: &steps, CFG: &cfg, Sampler: "euler", Scheduler: "simple"},
	})
	require.NoError(t, err)
	res.File.Discard()
	g := f.Graph()
	require.Contains(t, g, "x2_ks", "12 s is 192 frames: 3 chunks")
	for _, id := range []string{"3", "x1_ks", "x2_ks"} {
		assert.Equal(t, "777", string(g[id].Inputs["seed"]), id)
		assert.Equal(t, "12", string(g[id].Inputs["steps"]), id)
		assert.Equal(t, "4.5", string(g[id].Inputs["cfg"]), id)
		assert.Equal(t, `"euler"`, string(g[id].Inputs["sampler_name"]), id)
		assert.Equal(t, `"simple"`, string(g[id].Inputs["scheduler"]), id)
	}
	assert.NotContains(t, g, "x3_ks")
	assert.Equal(t, "192", string(g["96"].Inputs["length"]))
}
