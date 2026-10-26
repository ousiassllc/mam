package paths

import "net/url"

// ModelManager is the model manager screen (the bundled ComfyUI engine, the model catalog, trial generations),
// "/model-manager". GET answers the page (a full page, or the body alone for an HTMX request).
func ModelManager() string { return "/model-manager" }

// ModelManagerPart is "/model-manager/<rest...>": a section or an action of the screen ("engine", "models/:id/install",
// "trials/:id/file", ...). Every segment is escaped.
func ModelManagerPart(rest ...string) string {
	p := ModelManager()
	for _, s := range rest {
		p += "/" + url.PathEscape(s)
	}
	return p
}
