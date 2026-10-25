package paths

// The tools of the cards of the home page (spec/components/home-screen.md §3) whose status the page reads after it is
// shown (ToolStatus). The other cards (the generation test, the analytics) have no status.
const (
	ToolNodes        = "nodes"
	ToolWorkflows    = "workflows"
	ToolModelManager = "model-manager"
	ToolSettings     = "settings"
	ToolReviews      = "reviews"
)

// ToolStatus is "/tools/<tool>/status": GET answers the fragment that fills the status of the card of the tool on the
// home page. The tool is path-escaped; the router takes it as ":tool" (ToolStatus(":tool")).
func ToolStatus(tool string) string { return join("/tools", tool, "status") }
