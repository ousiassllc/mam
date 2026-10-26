package comfyui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
)

// The generation test names the seed and the negative prompt of a request: the workflow gets them as they are.
func TestImageRequestSeedAndNegativePromptReachTheWorkflow(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl", nil)
	seed := int64(424242)
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "a fox", AspectRatio: "1:1", Seed: &seed, NegativePrompt: "blurry hands"})
	require.NoError(t, err)

	g := f.Graph()
	assert.Equal(t, 424242.0, num(g["3"].Inputs["seed"]))
	assert.Equal(t, "blurry hands", str(g["7"].Inputs["text"]))
}

func TestVideoRequestSeedReachesTheWorkflow(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2video-wan22-5b", nil)
	seed := int64(7)
	out, err := b.GenerateVideo(ctx(t), "", modeladapter.VideoRequest{Prompt: "waves", AspectRatio: "16:9", Resolution: "480p", DurationSeconds: 3, Seed: &seed})
	require.NoError(t, err)
	out.File.Discard()

	for _, n := range f.Graph() {
		if raw, ok := n.Inputs["seed"]; ok {
			assert.Equal(t, 7.0, num(raw))
		}
		if raw, ok := n.Inputs["noise_seed"]; ok {
			assert.Equal(t, 7.0, num(raw))
		}
	}
}
