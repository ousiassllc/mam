// Package stage rewrites a ComfyUI editing workflow for the reference images of one run (localcfg.ModeEdit): the references
// that go into the conditioning and how long each stays in it. The helpers Link, LinkOf and Literal are the JSON of a graph's
// inputs, shared with the package that builds the run's graph.
package stage

import "encoding/json"

// Link is the JSON of a link to the output idx of the node id.
func Link(id string, idx int) json.RawMessage {
	raw, _ := json.Marshal([]any{id, idx})
	return raw
}

// LinkOf reads a link value; ok is false for a literal.
func LinkOf(raw json.RawMessage) (id string, idx int, ok bool) {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) != 2 {
		return "", 0, false
	}
	if json.Unmarshal(arr[0], &id) != nil || json.Unmarshal(arr[1], &idx) != nil {
		return "", 0, false
	}
	return id, idx, true
}

// Literal is the JSON of a literal input value.
func Literal(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
