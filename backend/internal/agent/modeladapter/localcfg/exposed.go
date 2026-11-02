package localcfg

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
)

// exposedLookup reads the inputs of g for package exposed.
func exposedLookup(g Graph) exposed.Lookup {
	return func(node, input string) (json.RawMessage, bool, bool) {
		v, ok := g[node].Inputs[input]
		return v, ok && IsLink(v), ok
	}
}

// validateExposed checks the exposed inputs of the workflow against its graph and its assignments (an input an assignment feeds cannot be exposed).
func validateExposed(where string, w Workflow, g Graph) []error {
	return exposed.Validate(where, w.ExposedInputs, exposedLookup(g), func(node, input string) bool {
		for _, a := range w.Assignments {
			if a.Node == node && a.Input == input {
				return true
			}
		}
		return false
	})
}

// ExposedCaps lists the exposed inputs of the workflow with the value its graph holds (nil when it exposes none).
func (w Workflow) ExposedCaps() []exposed.Cap {
	if len(w.ExposedInputs) == 0 {
		return nil
	}
	g, err := ParseGraph(w.Graph)
	if err != nil {
		return nil
	}
	return exposed.Caps(w.ExposedInputs, exposedLookup(g))
}

// GraphWith is the graph of the workflow with the values of its exposed inputs put in (by exposed.Input.Key); a value that is out of range, or
// whose key is not an exposed input, is an error. Without values it is the graph as it stands.
func (w Workflow) GraphWith(values map[string]any) (Graph, error) {
	g, err := ParseGraph(w.Graph)
	if err != nil || len(values) == 0 {
		return g, err
	}
	err = exposed.Apply(w.ExposedInputs, values, func(node, input string, raw json.RawMessage) { g[node].Inputs[input] = raw })
	return g, err
}
