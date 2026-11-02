package comfyui

import (
	"encoding/json"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/stage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// minDenoise keeps an img2img run from returning the reference unchanged.
const minDenoise = 0.02

// loraNode is the ID of the node a LoRA of the request is inserted as.
const loraNode = "mam_lora"

// denoise is the denoise an img2img workflow runs with: the more strongly the reference shows, the less of it the sampler
// replaces (1 - (1-floor)*strength, never below minDenoise). A run without a reference samples everything.
func (v values) denoise() float64 {
	if len(v.references) == 0 {
		return 1
	}
	return max(minDenoise, v.denoiseFloor, 1-(1-v.denoiseFloor)*v.opts.Ref(0).StrengthOrDefault())
}

// dropNode removes a node and the inputs of other nodes that link to it.
func dropNode(g localcfg.Graph, id string) {
	delete(g, id)
	for _, n := range g {
		for name, raw := range n.Inputs {
			if to, _, ok := stage.LinkOf(raw); ok && to == id {
				delete(n.Inputs, name)
			}
		}
	}
}

// dropUnused takes out the nodes of the reference assignments the run has no image for: an optional slot goes alone (its
// consumer keeps the other slots) with the plumbing that only it fed (dropOrphans); with no reference at all the NoReference
// overrides are applied first and everything that is fed by a reference node goes too (the VAEEncode of an img2img workflow).
func dropUnused(g localcfg.Graph, wf localcfg.Workflow, v values) {
	if len(v.references) == 0 {
		for _, o := range wf.NoReference {
			g[o.Node].Inputs[o.Input] = o.Value
		}
	}
	for _, a := range wf.Assignments {
		if a.Param != localcfg.ParamReferenceImage || a.Index < len(v.references) {
			continue
		}
		dropNode(g, a.Node)
	}
	dropOrphans(g)
	if len(v.references) == 0 {
		dropReferencePlumbing(g)
	}
}

// dropOrphans removes what a dropped reference left without its input, again until nothing is left: an image scaler or a
// VAEEncode that lost its image goes, and a ReferenceLatent that lost its latent is a pass-through, so the nodes that took it
// take its conditioning (a FLUX.2 workflow chains one ReferenceLatent per reference slot).
func dropOrphans(g localcfg.Graph) {
	for changed := true; changed; {
		changed = false
		for id, n := range g {
			switch {
			case (n.ClassType == "ImageScale" || n.ClassType == "ImageScaleToTotalPixels") && n.Inputs["image"] == nil,
				n.ClassType == "VAEEncode" && n.Inputs["pixels"] == nil:
				dropNode(g, id)
			case n.ClassType == "ReferenceLatent" && n.Inputs["latent"] == nil && n.Inputs["conditioning"] != nil:
				bypass(g, id)
			default:
				continue
			}
			changed = true
			break // the graph changed under the loop
		}
	}
}

// bypass removes a node with one pass-through input, "conditioning": what linked to the node now links to the node's source.
func bypass(g localcfg.Graph, id string) {
	src := g[id].Inputs["conditioning"]
	delete(g, id)
	for _, n := range g {
		for name, raw := range n.Inputs {
			if to, _, ok := stage.LinkOf(raw); ok && to == id {
				n.Inputs[name] = src
			}
		}
	}
}

// dropReferencePlumbing removes the reference nodes that no node uses any more (the NoReference overrides cut them off), and
// then the ones that only they used.
func dropReferencePlumbing(g localcfg.Graph) {
	for changed := true; changed; {
		changed = false
		used := map[string]bool{}
		for _, n := range g {
			for _, raw := range n.Inputs {
				if to, _, ok := stage.LinkOf(raw); ok {
					used[to] = true
				}
			}
		}
		for id, n := range g {
			if !used[id] && !isOutput(n) && isReferencePlumbing(n) {
				delete(g, id)
				changed = true
			}
		}
	}
}

// isOutput says a node saves or previews a result: it has no user and must stay.
func isOutput(n localcfg.Node) bool {
	return strings.HasPrefix(n.ClassType, "Save") || strings.HasPrefix(n.ClassType, "Preview")
}

// isReferencePlumbing is a node an img2img workflow adds to carry the reference image to the sampler.
func isReferencePlumbing(n localcfg.Node) bool {
	switch n.ClassType {
	case "LoadImage", "ImageScale", "VAEEncode":
		return true
	}
	return false
}

// applyLoRA inserts a LoraLoaderModelOnly behind the model node: everything that took that node's model now takes the LoRA's.
func applyLoRA(g localcfg.Graph, anchor string, l *localcfg.LoRA) {
	if l == nil || l.Name == "" || anchor == "" {
		return
	}
	strength := l.Strength
	if strength <= 0 {
		strength = localcfg.DefaultLoRAStrength
	}
	for _, n := range g {
		for name, raw := range n.Inputs {
			if to, idx, ok := stage.LinkOf(raw); ok && to == anchor && idx == 0 {
				n.Inputs[name] = stage.Link(loraNode, 0)
			}
		}
	}
	g[loraNode] = localcfg.Node{ClassType: "LoraLoaderModelOnly", Inputs: map[string]json.RawMessage{
		"lora_name": stage.Literal(l.Name), "strength_model": stage.Literal(strength), "model": stage.Link(anchor, 0),
	}}
}
