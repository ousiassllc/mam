package localcfg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

// SpeedMode is one way a workflow can sample (Wan 2.2 with the LightX2V LoRA: 4 steps at cfg 1, or the usual 20 steps at cfg 3.5
// without the LoRA; Qwen-Image-Edit 2511 with the Lightning LoRA: 4 or 8 steps at cfg 1, or the usual 20 steps at cfg 4). The graph of
// the workflow holds the values of its first mode; the other modes list what they change in it. A request names the mode it wants
// (VideoRequest.SpeedMode, ImageRequest.Options.SpeedMode); without one the first mode runs.
type SpeedMode struct {
	// ID is what a request names: lower-case letters, digits and "-" ("fast", "standard").
	ID string `json:"id"`
	// Label is what the screen and the MCP tools show ("4 ステップ（LightX2V）").
	Label string `json:"label"`
	// Overrides are put into the graph for this mode: a number of steps, the cfg, and the link that leaves a LoRA out.
	Overrides []Override `json:"overrides,omitempty"`
}

var speedModeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ValidSpeedModeID reports whether s can name a speed mode.
func ValidSpeedModeID(s string) bool { return speedModeIDPattern.MatchString(s) }

// SpeedMode returns the mode of the workflow named id; the empty id is the first (the default). ok is false for an unknown id or a
// workflow without modes.
func (w Workflow) SpeedMode(id string) (SpeedMode, bool) {
	if id == "" && len(w.SpeedModes) > 0 {
		return w.SpeedModes[0], true
	}
	i := slices.IndexFunc(w.SpeedModes, func(m SpeedMode) bool { return m.ID == id })
	if i < 0 {
		return SpeedMode{}, false
	}
	return w.SpeedModes[i], true
}

// validateSpeedModes checks the modes against the graph: each with a distinct ID and a label, the first one without overrides (it is
// the graph itself), and every override naming an input of a node of the workflow with a JSON value or a link to a node of the workflow.
func validateSpeedModes(where string, w Workflow, g Graph) []error {
	if len(w.SpeedModes) == 0 {
		return nil
	}
	var errs []error
	seen := map[string]bool{}
	for i, m := range w.SpeedModes {
		at := fmt.Sprintf("%s: speed_modes[%d]", where, i)
		switch {
		case !ValidSpeedModeID(m.ID):
			errs = append(errs, fmt.Errorf("%s: id must be lower-case letters, digits and -", at))
		case seen[m.ID]:
			errs = append(errs, fmt.Errorf("%s: id %q is listed twice", at, m.ID))
		case m.Label == "":
			errs = append(errs, fmt.Errorf("%s: label is required", at))
		case i == 0 && len(m.Overrides) > 0:
			errs = append(errs, fmt.Errorf("%s: the first mode is the graph itself and has no overrides", at))
		}
		seen[m.ID] = true
		for j, o := range m.Overrides {
			n, ok := g[o.Node]
			if !ok || n.Inputs[o.Input] == nil || len(o.Value) == 0 || !json.Valid(o.Value) {
				errs = append(errs, fmt.Errorf("%s: overrides[%d] must name an input of a node of the workflow and a JSON value", at, j))
				continue
			}
			if to, linked := linkTarget(o.Value); linked {
				if _, exists := g[to]; !exists {
					errs = append(errs, fmt.Errorf("%s: overrides[%d] links to the missing node %s", at, j, to))
				}
			}
		}
	}
	return errs
}
