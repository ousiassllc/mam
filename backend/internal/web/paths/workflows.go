package paths

// Nodes is the node screen, "/nodes": the ComfyUI node UI embedded in an iframe (spec/architecture/comfy/comfy-embedding.md).
// GET answers the page; "?endpoint=<name>" picks a registered external ComfyUI (default: the bundled one).
func Nodes() string { return "/nodes" }

// NodesStart is "/nodes/start": POST starts the bundled ComfyUI.
func NodesStart() string { return Nodes() + "/start" }

// Workflows is the screen of the ComfyUI workflows saved in mam, "/workflows".
func Workflows() string { return "/workflows" }

// Workflow is "/workflows/<id>" followed by the segments: Workflow(id, "open") is "/workflows/<id>/open".
func Workflow(id string, segments ...string) string {
	return join(Workflows(), append([]string{id}, segments...)...)
}

// WorkflowsPart is "/workflows/<rest...>": an action of the screen that has no workflow ID ("import", "remote").
func WorkflowsPart(rest ...string) string {
	p := Workflows()
	for _, r := range rest {
		p += "/" + r
	}
	return p
}
