// Package audiotest tests how the ComfyUI backend gives a speech-to-video workflow its sound (Issue #1153) through the fake server. It is a package of its own
// because the directory of the backend's tests is full (linterly).
package audiotest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

var (
	wav = []byte("RIFF\x24\x00\x00\x00WAVEfmt  a short sound")
	mp3 = []byte("ID3\x04\x00\x00\x00\x00\x00\x00 a short sound")
)

// s2v is the backend of the speech-to-video template over a fake server whose output is the template's own video node.
func s2v(t *testing.T) (*comfyui.Backend, *comfyuitest.Fake) {
	t.Helper()
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "113", []byte("mp4")
	tpl, ok := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	require.True(t, ok)
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: tpl.Model}, f
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func str(raw json.RawMessage) string { var s string; _ = json.Unmarshal(raw, &s); return s }

func request(audio *modeladapter.Input) modeladapter.VideoRequest {
	return modeladapter.VideoRequest{
		Prompt: "she greets us", DurationSeconds: 5, Resolution: "480p", AspectRatio: "16:9", Audio: audio,
		References: []modeladapter.Input{{Data: comfyuitest.PNG, MimeType: "image/png"}},
	}
}

// The sound is uploaded like a picture, with the extension of its kind (the server lists the audio files of its input folder by it) and the type it came
// with, and the graph's LoadAudio reads the name the server stored it under, while the picture goes to LoadImage.
func TestTheSoundIsUploadedWithTheExtensionOfItsKindAndFedToLoadAudio(t *testing.T) {
	for _, c := range []struct {
		name, mime, ext string
		data            []byte
	}{
		{"wav", "audio/wav", ".wav", wav}, {"mp3", "audio/mpeg", ".mp3", mp3},
		{"flac", "audio/flac", ".flac", audiofiletest.FLAC(44100, 44100)},
		{"ogg", "audio/ogg", ".ogg", audiofiletest.OGGOpus(312, 48000)},
		{"m4a", "audio/mp4", ".m4a", audiofiletest.M4A(0, 44100, 44100, true)},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, f := s2v(t)
			res, err := b.GenerateVideo(ctx(t), "", request(&modeladapter.Input{Data: c.data, MimeType: c.mime}))
			require.NoError(t, err)
			res.File.Discard()

			uploads, bodies, types := f.Uploads(), f.UploadBodies(), f.UploadTypes()
			require.Len(t, uploads, 2, "the picture and the sound")
			g := f.Graph()
			sound := str(g["58"].Inputs["audio"])
			assert.Contains(t, uploads, sound, "LoadAudio reads the name the sound was uploaded as")
			assert.Equal(t, c.ext, sound[len(sound)-len(c.ext):])
			var at int
			for i, u := range uploads {
				if u == sound {
					at = i
				}
			}
			assert.Equal(t, c.data, bodies[at])
			assert.Equal(t, c.mime, types[at])
			picture := str(g["52"].Inputs["image"])
			assert.NotEqual(t, sound, picture)
			assert.Equal(t, ".png", picture[len(picture)-4:], "the picture is not mistaken for the sound")
			assert.Equal(t, 1, f.Count("POST /prompt"))
		})
	}
}

// A workflow that makes the clip from a sound cannot run without one: the request is refused with the 422 of a missing input before anything is uploaded or
// queued, so that the Adapter may route to another model.
func TestAWorkflowWithASoundNeedsOneBeforeAnythingIsSent(t *testing.T) {
	b, f := s2v(t)
	_, err := b.GenerateVideo(ctx(t), "", request(nil))
	require.Error(t, err)
	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, http.StatusUnprocessableEntity, pe.Status)
	assert.Contains(t, pe.Message, "the workflow needs audio")
	assert.Zero(t, f.Count("POST /upload/image"), "not even the picture is uploaded")
	assert.Zero(t, f.Count("POST /prompt"))
}
