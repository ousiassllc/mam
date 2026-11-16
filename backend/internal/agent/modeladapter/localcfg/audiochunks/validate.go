package audiochunks

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

// MinChunkLength is the least "length" Wan 2.2 S2V takes (its own check: 73 frames).
const MinChunkLength = 73

// Validate checks the Spec against the base graph (where names the model in the messages): the chunk length is one Wan takes, the nodes it names exist and are
// of the class it needs, the extension's links lead to nodes of the extension or of the base graph, its ids do not clash with the base's, it uses Prev
// (a chunk that does not continue the clip is no extension), and something in the base graph takes the latent of Start (the node the chain is put in front of).
func (s Spec) Validate(where string, base map[string]Node) []error {
	var errs []error
	fail := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: audio_chunks: "+format, append([]any{where}, a...)...))
	}
	if s.ChunkLength < MinChunkLength || (s.ChunkLength-1)%FramesPerLatent != 0 {
		fail("chunk_length must be 4n+1 and at least %d (Wan's own limits), not %d", MinChunkLength, s.ChunkLength)
	}
	need := func(field, id, class string) {
		switch n, ok := base[id]; {
		case !ok:
			fail("%s names the node %q, which the graph does not have", field, id)
		case class != "" && n.ClassType != class:
			fail("%s is the node %s (%s), which must be a %s", field, id, n.ClassType, class)
		}
	}
	need("start", s.Start, "")
	need("sampler", s.Sampler, "KSampler")
	need("trim", s.Trim, "ImageFromBatch")
	if n, ok := base[s.Trim]; ok {
		if !literal(n.Inputs["length"]) {
			fail("trim %s needs a literal length input: mam puts the frame count there", s.Trim)
		}
		if _, _, isLink := link(n.Inputs["image"]); !isLink {
			fail("trim %s needs an image input linked to the decoder", s.Trim)
		}
	}
	if len(s.Extension) == 0 {
		fail("extension has no nodes")
	} else if _, ok := s.Extension[s.Output]; !ok {
		fail("output names the node %q, which the extension does not have", s.Output)
	}
	usesPrev := false
	for _, id := range slices.Sorted(maps.Keys(s.Extension)) {
		n := s.Extension[id]
		if _, clash := base[id]; clash || id == "" {
			fail("the extension node id %q is empty or is also a node of the graph", id)
		}
		if n.ClassType == "" {
			fail("the extension node %s has no class_type", id)
		}
		for _, in := range slices.Sorted(maps.Keys(n.Inputs)) {
			raw := n.Inputs[in]
			if prev(raw) {
				usesPrev = true
			}
			if to, _, ok := link(raw); ok {
				_, inExt := s.Extension[to]
				if _, inBase := base[to]; !inExt && !inBase {
					fail("extension node %s input %s links to the missing node %s", id, in, to)
				}
			}
		}
		if n.ClassType == "WanSoundImageToVideoExtend" {
			var length int
			if err := json.Unmarshal(n.Inputs["length"], &length); err != nil || length != s.ChunkLength {
				fail("extension node %s needs a literal length equal to chunk_length %d", id, s.ChunkLength)
			}
		}
	}
	if !usesPrev {
		fail("no input of the extension is %q: a chunk must continue the clip so far", Prev)
	}
	if !slices.ContainsFunc(slices.Collect(maps.Values(base)), func(n Node) bool { return takes(n, s.Start) }) {
		fail("nothing in the graph takes the latent of start %s, so there is nothing to continue", s.Start)
	}
	return errs
}

// takes reports whether a node has an input linked to the output 0 of the node id.
func takes(n Node, id string) bool {
	for _, raw := range n.Inputs {
		if to, idx, ok := link(raw); ok && to == id && idx == 0 {
			return true
		}
	}
	return false
}

func link(raw json.RawMessage) (id string, idx int, ok bool) {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) != 2 {
		return "", 0, false
	}
	if json.Unmarshal(arr[0], &id) != nil || json.Unmarshal(arr[1], &idx) != nil {
		return "", 0, false
	}
	return id, idx, true
}

func prev(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil && s == Prev
}

// literal reports whether an input is present and not a link.
func literal(raw json.RawMessage) bool {
	_, _, isLink := link(raw)
	return len(raw) > 0 && !isLink
}
