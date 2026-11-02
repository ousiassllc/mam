package comfyui

import (
	"encoding/json"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// minDenoise keeps an img2img run from returning the reference unchanged.
const minDenoise = 0.02

// loraNode is the ID of the node a LoRA of the request is inserted as.
const loraNode = "mam_lora"

// link is the JSON of a link to the output idx of the node id.
func link(id string, idx int) json.RawMessage {
	raw, _ := json.Marshal([]any{id, idx})
	return raw
}

// linkOf reads a link value; ok is false for a literal.
func linkOf(raw json.RawMessage) (id string, idx int, ok bool) {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) != 2 {
		return "", 0, false
	}
	if json.Unmarshal(arr[0], &id) != nil || json.Unmarshal(arr[1], &idx) != nil {
		return "", 0, false
	}
	return id, idx, true
}

// literal is the JSON of a literal input value.
func literal(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

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
			if to, _, ok := linkOf(raw); ok && to == id {
				delete(n.Inputs, name)
			}
		}
	}
}

// dropUnused takes out the nodes of the reference assignments the run has no image for: an optional slot goes alone (its
// consumer keeps the other slots); with no reference at all the NoReference overrides are applied first and everything that
// is fed by a reference node goes too (the VAEEncode of an img2img workflow).
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
	if len(v.references) == 0 {
		dropReferencePlumbing(g)
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
				if to, _, ok := linkOf(raw); ok {
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
			if to, idx, ok := linkOf(raw); ok && to == anchor && idx == 0 {
				n.Inputs[name] = link(loraNode, 0)
			}
		}
	}
	g[loraNode] = localcfg.Node{ClassType: "LoraLoaderModelOnly", Inputs: map[string]json.RawMessage{
		"lora_name": literal(l.Name), "strength_model": literal(strength), "model": link(anchor, 0),
	}}
}
