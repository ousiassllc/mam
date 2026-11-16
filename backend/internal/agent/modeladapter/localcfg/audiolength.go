package localcfg

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/audiochunks"
)

// AudioLength reports whether the clip of the workflow is as long as its sound (Workflow.AudioChunks): the length of a request does not apply, the sound's does.
func (w Workflow) AudioLength() bool { return w.AudioChunks != nil }

// ExtensionNodes are the nodes of the audio extension (nil when the workflow has none), by id: they are nodes of the run's graph too, so the checks of which
// classes a workflow uses (core nodes, custom nodes) read them with Graph.
func (w Workflow) ExtensionNodes() []Node {
	if w.AudioChunks == nil {
		return nil
	}
	nodes := make([]Node, 0, len(w.AudioChunks.Extension))
	for _, id := range slices.Sorted(maps.Keys(w.AudioChunks.Extension)) {
		nodes = append(nodes, Node(w.AudioChunks.Extension[id]))
	}
	return nodes
}

// validateAudioChunks checks the audio-length part of a workflow: a video workflow with a sound to measure, and a Spec that fits the graph (audiochunks.Spec.Validate).
func validateAudioChunks(where string, media Media, w Workflow, g Graph) []error {
	if w.AudioChunks == nil {
		return nil
	}
	var errs []error
	if media != MediaVideo {
		errs = append(errs, fmt.Errorf("%s: audio_chunks belongs to a video workflow", where))
	}
	if !w.Has(ParamAudio) {
		errs = append(errs, fmt.Errorf("%s: audio_chunks needs an assignment of the audio: the clip is as long as the sound", where))
	}
	base := make(map[string]audiochunks.Node, len(g))
	for id, n := range g {
		base[id] = audiochunks.Node(n)
	}
	return append(errs, w.AudioChunks.Validate(where, base)...)
}
