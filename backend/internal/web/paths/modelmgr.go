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
	MMCompanion     = "companion"
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

// CompanionFFmpeg is "/model-manager/companion/ffmpeg", the ffmpeg section of the companion software (#1106): GET answers the section, PUT saves the paths of ffmpeg and
// ffprobe.
func CompanionFFmpeg() string { return ModelManagerPart(MMCompanion, "ffmpeg") }

// FFmpegDownload is "/model-manager/companion/ffmpeg/download": GET answers the panel of the download (it polls itself while one runs), POST downloads the pinned
// ffmpeg and ffprobe, DELETE removes the downloaded copy.
func FFmpegDownload() string { return CompanionFFmpeg() + "/download" }

// FFmpegDownloadCancel is "/model-manager/companion/ffmpeg/download/cancel": POST stops the download (it resumes later).
func FFmpegDownloadCancel() string { return FFmpegDownload() + "/cancel" }

// CompanionFaceFusion is "/model-manager/companion/facefusion": GET looks for FaceFusion again and answers the section (it saves nothing and runs nothing), PUT saves the
// Python and the folder. mam never fetches FaceFusion, so there is no install route.
func CompanionFaceFusion() string { return ModelManagerPart(MMCompanion, "facefusion") }

// CompanionIndex is "/model-manager/companion/index", the list of the companion software alone (what its refresh button reads).
func CompanionIndex() string { return ModelManagerPart(MMCompanion, "index") }

// FFmpegSettings is the place where ffmpeg is set up and fetched: its section on the page of the companion software (the link of an error, of a job and of the
// first-run guide).
func FFmpegSettings() string { return ModelManagerCategory(MMCompanion) + "#mm-ffmpeg" }

// FaceFusionSettings is the place where FaceFusion is looked for and its way in is told: its section on the page of the companion software.
func FaceFusionSettings() string { return ModelManagerCategory(MMCompanion) + "#mm-facefusion" }
