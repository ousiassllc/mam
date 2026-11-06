package paths

import "net/url"

// ModelManager is the model manager screen (the bundled ComfyUI engine, the model catalog),
// "/model-manager". GET answers the overview (a full page, or the body alone for an HTMX request); a category is "/model-manager/<slug>" (ModelManagerPart).
func ModelManager() string { return "/model-manager" }

// The slugs of the categories of the model manager (the pages "/model-manager/<slug>"). organisms/modelmgr/mmcat.All is the one list of them; they are here
// so that the places that only link to a category (the job list, the reasons a model cannot be chosen, the generation test) need not import the screen.
// A slug is never the word of a route of a part of the screen ("engine", "models", "runtimes", "nodes", "face-models", "hf", "external", "overview"...).
const (
	MMComfyUI       = "comfyui"
	MMImageVideo    = "image-video"
	MMCustomNodes   = "custom-nodes"
	MMLocalRuntimes = "local-runtimes"
	MMFaceCheck     = "face-check"
	MMHFExternal    = "hf-external"
)

// ModelManagerCategory is the page of a category of the model manager, "/model-manager/<slug>".
func ModelManagerCategory(slug string) string { return ModelManagerPart(slug) }

// ModelManagerFaceModels is the section "顔チェック用モデル" of the model manager, on the page of its category (the link the generation test shows while the face
// similarity is off, #1026).
func ModelManagerFaceModels() string { return ModelManagerCategory(MMFaceCheck) + "#mm-face-models" }

// ModelManagerPart is "/model-manager/<rest...>": a section or an action of the screen ("engine", "models/:id/install", ...). Every segment is escaped.
func ModelManagerPart(rest ...string) string {
	p := ModelManager()
	for _, s := range rest {
		p += "/" + url.PathEscape(s)
	}
	return p
}
