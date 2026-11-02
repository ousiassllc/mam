package comfyui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
)

// The LTX-2.5 workflows (Issues #877, #927) run through the ComfyUI backend like the other video models, and the file they save is
// handed on as it is: the mp4 that SaveVideo wrote (video and the audio track together) reaches the stored result byte for byte, with its
// type, so the player of the screen plays the sound. The prompt, the seed and the size reach the nodes of the template.
func TestLTX25WorkflowsRunAndHandOnTheSavedVideoAsItIs(t *testing.T) {
	for _, id := range []string{"txt2video-ltx-2-5", "txt2video-ltx-2-5-uncensored", "img2video-ltx-2-5", "img2video-ltx-2-5-uncensored"} {
		t.Run(id, func(t *testing.T) {
			f := comfyuitest.New(t)
			f.OutputBody = []byte("mp4 with an audio track")
			b := backend(t, f, id, nil)
			req := modeladapter.VideoRequest{Prompt: "a dog barks", NegativePrompt: "blur", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"}
			first := id[:3] == "img"
			if first {
				req.FirstFrame = &modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"}
			}
			res, err := b.GenerateVideo(ctx(t), "", req)
			require.NoError(t, err)
			assert.Equal(t, "video/mp4", res.MimeType)
			assert.Equal(t, []byte("mp4 with an audio track"), filepath(t, res.File), "the saved file is not re-encoded: the sound stays in it")

			g := f.Graph()
			assert.Equal(t, "a dog barks", str(g["364"].Inputs["text"]))
			assert.Equal(t, "blur", str(g["373"].Inputs["text"]))
			assert.Equal(t, 24.0, num(g["370"].Inputs["fps"]))
			assert.Equal(t, 24.0, num(g["366"].Inputs["frame_rate"]), "the audio latent is made for the same frame rate")
			frames := num(g["356"].Inputs["length"])
			assert.Equal(t, frames, num(g["366"].Inputs["frames_number"]), "the picture and the sound are as long as each other")
			assert.Equal(t, 0, int(frames-1)%8)
			assert.Equal(t, 0, int(num(g["372"].Inputs["value"]))%64, "the width is a multiple of 64 (half of it a multiple of 32)")
			assert.Equal(t, 0, int(num(g["360"].Inputs["value"]))%64)
			assert.Equal(t, "auto", str(g["75"].Inputs["format"]))
			if first {
				assert.Regexp(t, `\.png$`, str(g["396"].Inputs["image"]))
				assert.Equal(t, 1, f.Count("POST /upload/image"))
			} else {
				assert.Zero(t, f.Count("POST /upload/image"))
			}
		})
	}
}
