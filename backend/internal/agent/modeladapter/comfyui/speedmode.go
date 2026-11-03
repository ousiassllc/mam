package comfyui

import (
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/stage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// applySpeedMode puts the overrides of the speed mode the request names into the graph (the empty id is the workflow's first mode,
// which is the graph itself), then drops the nodes that no output reaches any more (the LoRA a mode leaves out, and the LoRAs of the
// other modes while the first runs): the server is not asked to load a file the run does not use. A workflow without modes ignores
// the id; a request for a mode the workflow does not have is refused, as the generation test already told the user.
func applySpeedMode(g localcfg.Graph, wf localcfg.Workflow, id string) error {
	if len(wf.SpeedModes) == 0 {
		return nil
	}
	mode, ok := wf.SpeedMode(id)
	if !ok {
		return fmt.Errorf("the workflow has no speed mode %q", id)
	}
	for _, o := range mode.Overrides {
		g[o.Node].Inputs[o.Input] = o.Value
	}
	dropUnreachable(g, wf.OutputNode)
	return nil
}

// dropUnreachable keeps the nodes the output node depends on and removes the rest.
func dropUnreachable(g localcfg.Graph, output string) {
	keep := map[string]bool{}
	for todo := []string{output}; len(todo) > 0; {
		id := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if keep[id] {
			continue
		}
		keep[id] = true
		for _, raw := range g[id].Inputs {
			if to, _, ok := stage.LinkOf(raw); ok {
				todo = append(todo, to)
			}
		}
	}
	for id := range g {
		if !keep[id] {
			delete(g, id)
		}
	}
}
