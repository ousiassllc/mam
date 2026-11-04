package templatestest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Phr00t's Rapid AllInOne I2V v10 (Issue #1012) is one checkpoint (model, text encoder and VAE inside) run by the core nodes only: the loader feeds
// both encoders, WanImageToVideo (with the first frame) and the decoder; the sampler is the author's recommendation for v10 (euler_ancestral/beta,
// 4 steps, cfg 1, shift 8). There is no CLIP Vision (the author's example bypasses it) and no custom node.
func TestWan14BRapidAIOI2VTemplateIsOneCheckpointWithTheFirstFrame(t *testing.T) {
	const id = "img2video-wan22-14b-rapid-aio"
	g := graphOf(t, id)
	assert.Equal(t, []string{"26"}, idsOf(g, "CheckpointLoaderSimple"))
	assert.Equal(t, `"wan2.2-i2v-rapid-aio-v10-nsfw.safetensors"`, inputOf(g, "26", "ckpt_name"))
	for _, class := range []string{"UNETLoader", "CLIPLoader", "VAELoader", "CLIPVisionLoader", "WanVaceToVideo"} {
		assert.Empty(t, idsOf(g, class), class+": everything is in the checkpoint")
	}
	for _, enc := range []string{"6", "7"} {
		from, _ := linked(t, g, enc, "clip")
		assert.Equal(t, "26", from)
	}
	from, _ := linked(t, g, "57", "vae")
	assert.Equal(t, "26", from)
	from, n := linked(t, g, "57", "start_image")
	assert.Equal(t, "LoadImage", n.ClassType, from)
	from, _ = linked(t, g, "32", "model")
	assert.Equal(t, "26", from)
	assert.Equal(t, "8", inputOf(g, "32", "shift"))
	from, _ = linked(t, g, "3", "model")
	assert.Equal(t, "32", from)
	from, _ = linked(t, g, "3", "latent_image")
	assert.Equal(t, "57", from)
	from, _ = linked(t, g, "8", "vae")
	assert.Equal(t, "26", from)
	assert.Equal(t, "4", inputOf(g, "3", "steps"))
	assert.Equal(t, "1", inputOf(g, "3", "cfg"))
	assert.Equal(t, `"euler_ancestral"`, inputOf(g, "3", "sampler_name"))
	assert.Equal(t, `"beta"`, inputOf(g, "3", "scheduler"))
	tpl, _ := localcfg.TemplateByID(id)
	caps, ok := tpl.Model.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.FirstFrame)
	assert.Equal(t, 16, tpl.Model.Workflow.FPS)
}
