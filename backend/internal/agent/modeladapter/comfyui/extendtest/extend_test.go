// Package extendtest tests the graph of a workflow whose clip is as long as its sound (Issue #1153, comfyui/extend) with the shipped speech-to-video template.
// It is a package of its own because the directory of the backend's tests is full (linterly).
package extendtest_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/extend"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/stage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/audiochunks"
)

// graph is the template's graph with the values of a run in the sampler (a seed, the steps and cfg the screen chose), as the assignments leave it.
func graph(t *testing.T) (localcfg.Graph, audiochunks.Spec) {
	t.Helper()
	tpl, ok := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	require.True(t, ok)
	g, err := localcfg.ParseGraph(tpl.Model.Workflow.Graph)
	require.NoError(t, err)
	g["3"].Inputs["seed"] = stage.Literal(424242)
	g["3"].Inputs["steps"] = stage.Literal(12)
	g["3"].Inputs["cfg"] = stage.Literal(4.5)
	g["3"].Inputs["sampler_name"] = stage.Literal("euler")
	g["3"].Inputs["scheduler"] = stage.Literal("normal")
	return g, *tpl.Model.Workflow.AudioChunks
}

func link(t *testing.T, g localcfg.Graph, id, input string) string {
	t.Helper()
	to, _, ok := stage.LinkOf(g[id].Inputs[input])
	require.True(t, ok, "%s.%s is a link", id, input)
	return to
}

func literal(t *testing.T, g localcfg.Graph, id, input string) string {
	t.Helper()
	return string(g[id].Inputs[input])
}

// A sound that fits the first chunk adds nothing: the graph is the template's, with the trim at the frames of the sound.
func TestASoundOfOneChunkAddsNoNodeAndTrimsTheClip(t *testing.T) {
	g, spec := graph(t)
	before := len(g)
	require.NoError(t, extend.Apply(g, spec, 48)) // 3 s at 16 fps
	assert.Len(t, g, before)
	assert.Equal(t, "48", literal(t, g, "96", "length"))
	assert.Equal(t, "3", link(t, g, "80", "samples"), "the decoder still reads the first sampler")
	assert.Equal(t, "80", link(t, g, "96", "image"))
	assert.Equal(t, "96", link(t, g, "82", "images"))
}

// Each chunk after the first is the extension of the template under its own prefix; the decoder reads the whole chain, and every link is to the latent so far.
func TestAFurtherChunkContinuesTheLatentSoFar(t *testing.T) {
	g, spec := graph(t)
	before := len(g)
	require.NoError(t, extend.Apply(g, spec, 100)) // 2 chunks: 157 frames hold 100
	assert.Len(t, g, before+3)
	assert.Equal(t, "WanSoundImageToVideoExtend", g["x1_ext"].ClassType)
	assert.Equal(t, "KSampler", g["x1_ks"].ClassType)
	assert.Equal(t, "LatentConcat", g["x1_cat"].ClassType)

	assert.Equal(t, "3", link(t, g, "x1_ext", "video_latent"), "the first chunk's latent is what the first extension continues")
	assert.Equal(t, "3", link(t, g, "x1_cat", "samples1"))
	assert.Equal(t, "x1_ks", link(t, g, "x1_cat", "samples2"))
	for input, from := range map[string]string{"positive": "x1_ext", "negative": "x1_ext", "latent_image": "x1_ext"} {
		assert.Equal(t, from, link(t, g, "x1_ks", input), input)
	}
	// the extension is conditioned like the first chunk: the same text, vae, sound and picture, and the same model
	for input, from := range map[string]string{"positive": "6", "negative": "7", "vae": "39", "audio_encoder_output": "56", "ref_image": "52"} {
		assert.Equal(t, from, link(t, g, "x1_ext", input), input)
	}
	assert.Equal(t, "54", link(t, g, "x1_ks", "model"))
	assert.Equal(t, "77", literal(t, g, "x1_ext", "length"))
	assert.Equal(t, "x1_cat", link(t, g, "80", "samples"), "the decoder reads the whole clip")
	assert.Equal(t, "100", literal(t, g, "96", "length"))
	assert.Equal(t, "3", link(t, g, "x1_cat", "samples1"), "the first sampler is not moved")
	assert.Equal(t, "93", link(t, g, "3", "positive"))
}

// The sampler of every chunk samples as the first: the seed, steps, cfg, sampler and scheduler of the run are copied, the denoise stays 1.
func TestEveryExtensionSamplesWithTheValuesOfTheRun(t *testing.T) {
	g, spec := graph(t)
	require.NoError(t, extend.Apply(g, spec, 397)) // exactly 5 chunks
	for k := 1; k <= 4; k++ {
		id := fmt.Sprintf("x%d_ks", k)
		assert.Equal(t, "424242", literal(t, g, id, "seed"), id)
		assert.Equal(t, "12", literal(t, g, id, "steps"), id)
		assert.Equal(t, "4.5", literal(t, g, id, "cfg"), id)
		assert.Equal(t, `"euler"`, literal(t, g, id, "sampler_name"), id)
		assert.Equal(t, `"normal"`, literal(t, g, id, "scheduler"), id)
		assert.Equal(t, "1", literal(t, g, id, "denoise"), id)
	}
}

// Five chunks make a chain of four extensions: each continues the one before, and only the last is read by the decoder.
func TestFiveChunksMakeAChainOfFourExtensions(t *testing.T) {
	g, spec := graph(t)
	before := len(g)
	require.NoError(t, extend.Apply(g, spec, 397))
	assert.Len(t, g, before+12)
	assert.Equal(t, "3", link(t, g, "x1_ext", "video_latent"))
	for k := 2; k <= 4; k++ {
		prev := fmt.Sprintf("x%d_cat", k-1)
		assert.Equal(t, prev, link(t, g, fmt.Sprintf("x%d_ext", k), "video_latent"), "extension %d", k)
		assert.Equal(t, prev, link(t, g, fmt.Sprintf("x%d_cat", k), "samples1"), "extension %d", k)
		assert.Equal(t, fmt.Sprintf("x%d_ks", k), link(t, g, fmt.Sprintf("x%d_cat", k), "samples2"))
	}
	assert.Equal(t, "x4_cat", link(t, g, "80", "samples"))
	assert.Equal(t, "397", literal(t, g, "96", "length"))
	consumers := 0
	for id, n := range g {
		for _, raw := range n.Inputs {
			if to, _, ok := stage.LinkOf(raw); ok && to == "x4_cat" {
				consumers++
				assert.Equal(t, "80", id, "only the decoder takes the whole chain")
			}
		}
	}
	assert.Equal(t, 1, consumers)
	// one more frame needs a sixth chunk
	g2, _ := graph(t)
	require.NoError(t, extend.Apply(g2, spec, 398))
	assert.Equal(t, "x5_cat", link(t, g2, "80", "samples"))
	assert.Len(t, g2, before+15)
}

// A one-hour sound could not be trimmed: ImageFromBatch takes at most 4096 frames, so the trim node is bypassed and the surplus of the last chunk stays.
func TestAClipLongerThanTheTrimCanTakeIsNotTrimmed(t *testing.T) {
	g, spec := graph(t)
	require.NoError(t, extend.Apply(g, spec, audiochunks.MaxTrim+1))
	assert.NotContains(t, g, "96")
	assert.Equal(t, "x"+fmt.Sprint(spec.Chunks(audiochunks.MaxTrim+1)-1)+"_cat", link(t, g, "80", "samples"))
	assert.Equal(t, "80", link(t, g, "82", "images"), "the video takes the decoder's frames as they are")

	g, _ = graph(t)
	require.NoError(t, extend.Apply(g, spec, audiochunks.MaxTrim))
	assert.Equal(t, "4096", literal(t, g, "96", "length"), "up to the limit is trimmed")
}

// No length is the first chunk (a caller that did not say how long the clip is gets the clip of the template, not an empty one).
func TestWithoutALengthTheClipIsTheFirstChunk(t *testing.T) {
	g, spec := graph(t)
	before := len(g)
	require.NoError(t, extend.Apply(g, spec, 0))
	assert.Len(t, g, before)
	assert.Equal(t, "77", literal(t, g, "96", "length"))
}

func TestAnExtensionOfAGraphWithoutItsNodesIsRefused(t *testing.T) {
	g, spec := graph(t)
	delete(g, "96")
	assert.Error(t, extend.Apply(g, spec, 100))
	g, spec = graph(t)
	spec.Output = "nope"
	assert.Error(t, extend.Apply(g, spec, 100))
	g, spec = graph(t)
	require.NoError(t, extend.Apply(g, spec, 100))
	raw, err := json.Marshal(g)
	require.NoError(t, err)
	_, err = localcfg.ParseGraph(raw)
	assert.NoError(t, err, "every link of the built graph names a node of it")
}
