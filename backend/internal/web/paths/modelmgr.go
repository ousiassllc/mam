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
	MMLocalLLM      = "local-llm"
	MMFaceCheck     = "face-check"
	MMHFExternal    = "hf-external"
)

// ModelManagerCategory is the page of a category of the model manager, "/model-manager/<slug>".
func ModelManagerCategory(slug string) string { return ModelManagerPart(slug) }

// ModelManagerLocalLLMUse is "/model-manager/local-llm/use": PUT saves the server the calls of the provider "local" go to (field use: llama-cpp, ollama, custom) and answers
// with the section "使うサーバー"; GET reads it again (?part=state: the line of its state alone, which the section polls while a server installs or starts).
func ModelManagerLocalLLMUse() string { return ModelManagerPart(MMLocalLLM, "use") }

// ModelManagerLocalLLMConnection is "/model-manager/local-llm/connection": PUT saves the address, the optional API key and the unload switch of "別のサーバー" (fields base_url,
// api_key, clear_key, unload) and answers with the section.
func ModelManagerLocalLLMConnection() string { return ModelManagerPart(MMLocalLLM, "connection") }

// ModelManagerLocalLLMConnectionTest is "/model-manager/local-llm/connection/test": POST tries the server of the form (the typed base URL and key, else the saved ones) with
// GET /v1/models and answers with the result alone.
func ModelManagerLocalLLMConnectionTest() string { return ModelManagerLocalLLMConnection() + "/test" }

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
