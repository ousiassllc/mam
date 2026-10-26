package workflows

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// Suggest guesses the input assignment of a graph from the nodes the core ComfyUI workflows use: the first text
// encoder with a literal text is the prompt and the second the negative prompt, the sampler's seed, the empty
// latent's size (and length for a video), the Save node as the output. The user checks the result in the
// assignment editor; a graph the guess does not fit gets a partial or empty one.
func Suggest(graph json.RawMessage, kind localcfg.Media) localcfg.WorkflowSpec {
	g, err := localcfg.ParseGraph(graph)
	if err != nil {
		return localcfg.WorkflowSpec{}
	}
	ids := make([]string, 0, len(g))
	for id := range g {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int { return numericLess(a, b) })
	var spec localcfg.WorkflowSpec
	add := func(p localcfg.Param, node, input string) {
		spec.Assignments = append(spec.Assignments, localcfg.Assignment{Param: p, Node: node, Input: input})
	}
	texts := 0
	for _, id := range ids {
		n := g[id]
		switch {
		case strings.HasPrefix(n.ClassType, "CLIPTextEncode") && literal(n, "text") && texts < 2:
			add([]localcfg.Param{localcfg.ParamPrompt, localcfg.ParamNegativePrompt}[texts], id, "text")
			texts++
		case strings.HasPrefix(n.ClassType, "KSampler") && literal(n, "seed") && !has(spec, localcfg.ParamSeed):
			add(localcfg.ParamSeed, id, "seed")
		case n.ClassType == "RandomNoise" && literal(n, "noise_seed") && !has(spec, localcfg.ParamSeed):
			add(localcfg.ParamSeed, id, "noise_seed")
		case strings.HasPrefix(n.ClassType, "Empty") && strings.Contains(n.ClassType, "Latent") && !has(spec, localcfg.ParamWidth):
			if literal(n, "width") && literal(n, "height") {
				add(localcfg.ParamWidth, id, "width")
				add(localcfg.ParamHeight, id, "height")
				if kind == localcfg.MediaVideo && literal(n, "length") {
					add(localcfg.ParamFrames, id, "length")
				}
			}
		case spec.OutputNode == "" && isSaveNode(n.ClassType, kind):
			spec.OutputNode = id
		}
	}
	if kind == localcfg.MediaVideo {
		v := localcfg.DefaultVideoSettings()
		spec.Video = &v
	}
	return spec
}

func has(s localcfg.WorkflowSpec, p localcfg.Param) bool {
	return slices.ContainsFunc(s.Assignments, func(a localcfg.Assignment) bool { return a.Param == p })
}

// literal reports whether the input holds a value (not a link to another node).
func literal(n localcfg.Node, input string) bool {
	v, ok := n.Inputs[input]
	return ok && !localcfg.IsLink(v)
}

func isSaveNode(class string, kind localcfg.Media) bool {
	if kind == localcfg.MediaVideo {
		return class == "SaveVideo" || class == "SaveWEBM" || strings.HasPrefix(class, "VHS_VideoCombine")
	}
	return class == "SaveImage"
}

// numericLess orders node IDs by length and then text, so "9" comes before "10".
func numericLess(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}
