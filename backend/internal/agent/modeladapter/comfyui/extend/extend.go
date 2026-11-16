// Package extend makes the graph of a workflow with audio_chunks as long as its sound needs (Issue #1153; localcfg.Workflow.AudioChunks, package audiochunks): Comfy's own template
// of Wan 2.2 S2V (video_wan2_2_14B_s2v) chains one more "Extend" (WanSoundImageToVideoExtend, a KSampler, a LatentConcat) for every chunk of the sound after the first, and the
// graph of the workflow has the first chunk only. Apply adds the others and sets the number of frames the clip keeps.
package extend

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/stage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/audiochunks"
)

// samplerInputs are the inputs of the sampler node (Spec.Sampler) that the sampler nodes of the extension take over: the values of the run, so that every chunk is sampled as the first.
var samplerInputs = []string{"seed", "steps", "cfg", "sampler_name", "scheduler"}

// Prefix is the start of the ids of the nodes of the k-th extension (k from 1): "x1_", "x2_"...
func Prefix(k int) string { return "x" + strconv.Itoa(k) + "_" }

// Apply makes g the graph of a clip of the given number of frames, by the Spec, which must have passed Validate. frames <= 0 is a clip of the first chunk. Nothing in g changes
// before the checks pass. Call it after the values of the run are in the graph and before a LoRA is inserted (the LoRA then reaches the samplers of the extension too).
//
//  1. The number of chunks is the least that covers the frames (Spec.Chunks); one chunk adds nothing.
//  2. Whatever takes the output 0 of Spec.Start is moved to the output of the last extension (the decoder reads the whole clip), before the extension's nodes exist.
//  3. For every k from 1 to n-1 the extension is copied under the prefix "x<k>_", with Prev (the latent so far) being Start for the first copy and the previous copy's Output after that.
//  4. The sampler nodes of the extension get the sampling values of Spec.Sampler.
//  5. Spec.Trim keeps the first `frames` frames (ImageFromBatch clamps to what exists, so the surplus of the last chunk goes). ImageFromBatch takes at most
//     audiochunks.MaxTrim: a longer clip is not trimmed, the trim node is bypassed and the clip keeps the surplus of the last chunk (under 80 frames: 5 seconds).
func Apply(g localcfg.Graph, spec audiochunks.Spec, frames int) error {
	if g[spec.Start].ClassType == "" || g[spec.Sampler].ClassType == "" || g[spec.Trim].ClassType == "" {
		return fmt.Errorf("the workflow has no node %q, %q or %q (audio_chunks)", spec.Start, spec.Sampler, spec.Trim)
	}
	if frames <= 0 {
		frames = spec.ChunkLength
	}
	n := spec.Chunks(frames)
	if n > 1 {
		if _, ok := spec.Extension[spec.Output]; !ok {
			return fmt.Errorf("the extension has no node %q (audio_chunks.output)", spec.Output)
		}
		last := Prefix(n-1) + spec.Output
		repoint(g, spec.Start, last)
		prev := spec.Start
		for k := 1; k < n; k++ {
			add(g, spec, k, prev)
			prev = Prefix(k) + spec.Output
		}
	}
	if frames > audiochunks.MaxTrim {
		bypass(g, spec.Trim)
		return nil
	}
	g[spec.Trim].Inputs["length"] = stage.Literal(frames)
	return nil
}

// repoint makes every input that takes the output 0 of from take the output 0 of to.
func repoint(g localcfg.Graph, from, to string) {
	for _, node := range g {
		for name, raw := range node.Inputs {
			if id, idx, ok := stage.LinkOf(raw); ok && id == from && idx == 0 {
				node.Inputs[name] = stage.Link(to, 0)
			}
		}
	}
}

// add copies the extension of the spec into g as the k-th one, continuing the latent prev.
func add(g localcfg.Graph, spec audiochunks.Spec, k int, prev string) {
	prefix := Prefix(k)
	sampler := g[spec.Sampler]
	for id, src := range spec.Extension {
		inputs := make(map[string]json.RawMessage, len(src.Inputs))
		for name, raw := range src.Inputs {
			inputs[name] = raw
			var s string
			switch to, idx, isLink := stage.LinkOf(raw); {
			case json.Unmarshal(raw, &s) == nil && s == audiochunks.Prev:
				inputs[name] = stage.Link(prev, 0)
			case isLink:
				if _, own := spec.Extension[to]; own {
					inputs[name] = stage.Link(prefix+to, idx)
				}
			}
		}
		if src.ClassType == sampler.ClassType {
			for _, name := range samplerInputs {
				if v, has := sampler.Inputs[name]; has && !localcfg.IsLink(v) {
					inputs[name] = v
				}
			}
		}
		g[prefix+id] = localcfg.Node{ClassType: src.ClassType, Inputs: inputs}
	}
}

// bypass removes a node with an "image" input: what took its output 0 takes its image.
func bypass(g localcfg.Graph, id string) {
	src := g[id].Inputs["image"]
	delete(g, id)
	for _, node := range g {
		for name, raw := range node.Inputs {
			if to, idx, ok := stage.LinkOf(raw); ok && to == id && idx == 0 {
				node.Inputs[name] = src
			}
		}
	}
}
