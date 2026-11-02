package localcfg

import (
	"slices"
	"strings"
)

// InputInfo is one input of a node as the settings screen shows it when the user picks where a value goes.
type InputInfo struct {
	Name string `json:"name"`
	// Link is true when the input is fed by another node (a value cannot be put there).
	Link bool `json:"link"`
	// Value is the literal value, shortened (empty for a link).
	Value string `json:"value,omitempty"`
}

// NodeInfo is one node of a workflow.
type NodeInfo struct {
	ID     string      `json:"id"`
	Class  string      `json:"class_type"`
	Inputs []InputInfo `json:"inputs"`
}

// Inspect lists the nodes of a workflow (API format) with their inputs, by node ID in numeric order where
// the IDs are numbers.
func Inspect(raw []byte) ([]NodeInfo, error) {
	g, err := ParseGraph(raw)
	if err != nil {
		return nil, err
	}
	ids := sortedKeys(g)
	slices.SortStableFunc(ids, func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	})
	out := make([]NodeInfo, 0, len(ids))
	for _, id := range ids {
		n := g[id]
		info := NodeInfo{ID: id, Class: n.ClassType}
		for _, name := range sortedKeys(n.Inputs) {
			in := InputInfo{Name: name, Link: IsLink(n.Inputs[name])}
			if !in.Link {
				in.Value = shorten(string(n.Inputs[name]), 60)
			}
			info.Inputs = append(info.Inputs, in)
		}
		out = append(out, info)
	}
	return out, nil
}

func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
