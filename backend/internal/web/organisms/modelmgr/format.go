package modelmgr

import (
	"net/url"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// joinNames lists names in a sentence.
func joinNames(names []string) string { return strings.Join(names, "、") }

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// engineURL is where the engine card asks for itself again: with the job it reports on.
func engineURL(e Engine) string {
	if e.Job == "" {
		return paths.ModelManagerPart("engine")
	}
	return paths.ModelManagerPart("engine") + "?job=" + url.QueryEscape(e.Job)
}

// catalogURL is where the model list asks for itself again: with the jobs it reports on.
func catalogURL(c Catalog) string {
	if len(c.Jobs) == 0 {
		return paths.ModelManagerPart("models")
	}
	return paths.ModelManagerPart("models") + "?jobs=" + url.QueryEscape(strings.Join(c.Jobs, ","))
}

// modelRowSelector is the CSS selector of the row of a model: where the focus goes when the dialog that was asked about it
// is gone and the row was drawn again.
func modelRowSelector(id string) string {
	return `[data-model="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(id) + `"]`
}
