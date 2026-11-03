package paths

// ToolModelManager is the tool of the model manager card of the settings screen (spec/components/settings-screen.md §5),
// whose status the page reads after it is shown (ToolStatus).
const ToolModelManager = "model-manager"

// ToolStatus is "/tools/<tool>/status": GET answers the fragment that fills the status of the card of the tool. The
// tool is path-escaped; the router takes it as ":tool" (ToolStatus(":tool")).
func ToolStatus(tool string) string { return join("/tools", tool, "status") }
