package configtest

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/audiochunks"
)

// The frames of a clip of n chunks are Window*n-3 (each chunk is 20 latent frames of 4 frames, the first latent frame being one picture): the number of chunks is the
// least that covers the frames of the sound, and a sound that fits the first chunk needs no extension (Issue #1153).
func TestChunksCoverTheFramesOfTheSound(t *testing.T) {
	s := audiochunks.Spec{ChunkLength: 77}
	assert.Equal(t, 20, s.LatentFrames())
	assert.Equal(t, 80, s.Window())
	for frames, want := range map[int]int{1: 1, 48: 1, 77: 1, 78: 2, 80: 2, 157: 2, 158: 3, 237: 3, 238: 4, 397: 5, 398: 6, 960: 13, 9600: 121} {
		assert.Equal(t, want, s.Chunks(frames), "%d frames", frames)
	}
	assert.Equal(t, 1, s.Chunks(0), "no frames still makes the first chunk")
	assert.Equal(t, 1, s.Chunks(-5))
	// the chunk count is exact: n chunks decode to 80n-3 frames, which hold the sound, and n-1 chunks do not
	for frames := 1; frames <= 1200; frames++ {
		n := s.Chunks(frames)
		assert.GreaterOrEqual(t, 80*n-3, frames, "%d frames in %d chunks", frames, n)
		if n > 1 {
			assert.Less(t, 80*(n-1)-3, frames, "%d frames need all of %d chunks", frames, n)
		}
	}
	assert.Equal(t, 84, (audiochunks.Spec{ChunkLength: 81}).Window(), "another chunk length (21 latent frames) changes the window with it")
}

func TestFramesForRoundsTheSoundUpToWholeFrames(t *testing.T) {
	for _, c := range []struct {
		seconds float64
		fps     int
		want    int
	}{{3, 16, 48}, {3.1, 16, 50}, {60, 16, 960}, {0.04, 16, 1}, {0, 16, 1}, {5, 24, 120}, {4.2, 16, 68}} {
		assert.Equal(t, c.want, audiochunks.FramesFor(c.seconds, c.fps), "%g s at %d fps", c.seconds, c.fps)
	}
}

// variant is the speech-to-video model with its Spec (deep-copied) and its graph edited.
func variant(t *testing.T, spec func(*audiochunks.Spec), graph func(localcfg.Graph)) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID("audio2video-wan22-s2v-14b")
	require.True(t, ok)
	m := tpl.Model
	w := *m.Workflow
	var s audiochunks.Spec
	raw, err := json.Marshal(w.AudioChunks)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &s))
	if spec != nil {
		spec(&s)
	}
	w.AudioChunks = &s
	g, err := localcfg.ParseGraph(w.Graph)
	require.NoError(t, err)
	if graph != nil {
		graph(g)
	}
	w.Graph, err = json.Marshal(g)
	require.NoError(t, err)
	m.Workflow = &w
	return m
}

func problems(m localcfg.Model) string {
	return errors.Join(localcfg.ValidateModel("box/s2v", m, comfy())...).Error()
}

func TestAudioChunksOfTheShippedTemplateAreValid(t *testing.T) {
	m := variant(t, nil, nil)
	assert.Empty(t, localcfg.ValidateModel("box/s2v", m, comfy()))
	caps, ok := m.VideoCaps()
	require.True(t, ok)
	assert.True(t, caps.AudioLength, "the clip is as long as the sound")
	assert.True(t, caps.AudioInput)
	assert.True(t, m.Workflow.AudioLength())
	assert.Len(t, m.Workflow.ExtensionNodes(), 3, "the Extend, its sampler and the concat")
	assert.Equal(t, []string{"ImageFromBatch", "KSampler", "LatentConcat", "WanSoundImageToVideoExtend"}, extensionClasses(m))

	tpl, _ := localcfg.TemplateByID("img2video-wan22-5b")
	other, _ := tpl.Model.VideoCaps()
	assert.False(t, other.AudioLength, "a model whose clip has the length of the request")
	assert.Empty(t, tpl.Model.Workflow.ExtensionNodes())
}

func extensionClasses(m localcfg.Model) []string {
	var out []string
	for _, n := range m.Workflow.ExtensionNodes() {
		out = append(out, n.ClassType)
	}
	// the trim is a node of the graph, listed with them because the allowlist test must know it too
	g, _ := localcfg.ParseGraph(m.Workflow.Graph)
	out = append(out, g[m.Workflow.AudioChunks.Trim].ClassType)
	slices.Sort(out)
	return out
}

func TestAudioChunksAreRefusedWhenTheyDoNotFitTheGraph(t *testing.T) {
	cases := []struct {
		name  string
		spec  func(*audiochunks.Spec)
		graph func(localcfg.Graph)
		want  string
	}{
		{"chunk length not 4n+1", func(s *audiochunks.Spec) { s.ChunkLength = 76 }, nil, "chunk_length must be 4n+1 and at least 73"},
		{"chunk length too short", func(s *audiochunks.Spec) { s.ChunkLength = 69 }, nil, "chunk_length must be 4n+1 and at least 73"},
		{"extend length differs from the chunk length", func(s *audiochunks.Spec) { s.ChunkLength = 81 }, nil, "literal length equal to chunk_length 81"},
		{"no start", func(s *audiochunks.Spec) { s.Start = "nope" }, nil, `start names the node "nope"`},
		{"sampler is not a KSampler", func(s *audiochunks.Spec) { s.Sampler = "80" }, nil, "sampler is the node 80 (VAEDecode), which must be a KSampler"},
		{"trim is not an ImageFromBatch", func(s *audiochunks.Spec) { s.Trim = "80" }, nil, "trim is the node 80 (VAEDecode), which must be a ImageFromBatch"},
		{"no extension", func(s *audiochunks.Spec) { s.Extension = nil }, nil, "extension has no nodes"},
		{"no output", func(s *audiochunks.Spec) { s.Output = "nope" }, nil, `output names the node "nope"`},
		{"an id of the graph", func(s *audiochunks.Spec) { s.Extension["3"] = s.Extension["cat"] }, nil, `the extension node id "3" is empty or is also a node of the graph`},
		{"a link to nowhere", func(s *audiochunks.Spec) {
			s.Extension["ks"].Inputs["positive"] = json.RawMessage(`["zzz", 0]`)
		}, nil, "extension node ks input positive links to the missing node zzz"},
		{"no previous latent", func(s *audiochunks.Spec) {
			s.Extension["ext"].Inputs["video_latent"] = json.RawMessage(`["3", 0]`)
			s.Extension["cat"].Inputs["samples1"] = json.RawMessage(`["3", 0]`)
		}, nil, `no input of the extension is "$prev"`},
		{"trim length is a link", nil, func(g localcfg.Graph) { g["96"].Inputs["length"] = json.RawMessage(`["93", 0]`) }, "trim 96 needs a literal length input"},
		{"nothing takes the start", nil, func(g localcfg.Graph) { g["80"].Inputs["samples"] = json.RawMessage(`["93", 2]`) }, "nothing in the graph takes the latent of start 3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Contains(t, problems(variant(t, c.spec, c.graph)), c.want)
		})
	}
}

func TestAudioChunksNeedAVideoWorkflowWithASound(t *testing.T) {
	noSound := variant(t, nil, nil)
	w := *noSound.Workflow
	w.Assignments = slices.DeleteFunc(slices.Clone(w.Assignments), func(a localcfg.Assignment) bool { return a.Param == localcfg.ParamAudio })
	noSound.Workflow = &w
	assert.Contains(t, problems(noSound), "audio_chunks needs an assignment of the audio")

	image, _ := localcfg.TemplateByID("txt2img-sdxl")
	iw := *image.Model.Workflow
	iw.AudioChunks = variant(t, nil, nil).Workflow.AudioChunks
	im := image.Model
	im.Workflow = &iw
	assert.Contains(t, problems(im), "audio_chunks belongs to a video workflow")
}

// The nodes of the audio extension are nodes of the run's graph: they must be core nodes of ComfyUI (or come from a custom node the template requires) like the graph's own.
func TestTheExtensionNodesOfEveryTemplateAreAllowedToo(t *testing.T) {
	found := 0
	for _, tpl := range localcfg.Templates() {
		for _, n := range tpl.Model.Workflow.ExtensionNodes() {
			assertNodeAllowed(t, tpl, n.ClassType)
			found++
		}
	}
	assert.Equal(t, 3, found, "the Extend, its sampler and the concat of the speech-to-video template")
	assert.Contains(t, coreNodes, "WanSoundImageToVideoExtend")
	assert.Contains(t, coreNodes, "LatentConcat")
	assert.Contains(t, coreNodes, "ImageFromBatch")
}
