package paths

import "net/url"

// ModelManager is the model manager screen (the bundled ComfyUI engine, the model catalog),
// "/model-manager". GET answers the page (a full page, or the body alone for an HTMX request).
func ModelManager() string { return "/model-manager" }

// ModelManagerFaceModels is the section "顔チェック用モデル" of the model manager (the link the generation test shows while the face similarity is off, #1026).
func ModelManagerFaceModels() string { return ModelManager() + "#mm-face-models" }

// ModelManagerPart is "/model-manager/<rest...>": a section or an action of the screen ("engine", "models/:id/install", ...). Every segment is escaped.
func ModelManagerPart(rest ...string) string {
	p := ModelManager()
	for _, s := range rest {
		p += "/" + url.PathEscape(s)
	}
	return p
}
