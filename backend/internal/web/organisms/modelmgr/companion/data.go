// Package companion is the page of the companion software of the model manager (spec/components/model-manager-screen.md, "付随ソフトの導入（#1106）"): the one
// list of the software that needs one more install (what it is for, where it is installed, how it stands) and the sections of ffmpeg and FaceFusion (the
// subpackages ffmpeg and facefusion). mam fetches ffmpeg on a button; it never fetches FaceFusion, and nothing here installs it.
package companion

import (
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion/facefusion"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion/ffmpeg"
)

// IndexID is the list (it is also what its refresh button swaps).
const IndexID = "mm-companion-index"

// The ids of the sections of the page (the ids of the category in mmcat).
const (
	FFmpegID     = "mm-ffmpeg"
	FaceFusionID = "mm-facefusion"
)

// The data-tool of a row of the list, in the order of the list. Each owner of the software tells its state; nothing is stored for the list.
const (
	ToolFFmpeg       = "ffmpeg"
	ToolFaceFusion   = "facefusion"
	ToolFaceModels   = "face-models"
	ToolComfyUI      = "comfyui"
	ToolLocalRuntime = "local-runtimes"
	ToolCustomNodes  = "custom-nodes"
	ToolCodex        = "codex"
)

// The data-state of the rows that this package's owners tell (ffmpeg, FaceFusion, Codex). The other rows carry the state of the line of their own category on the
// overview (the phase of the engine card, the state of the face-check section, empty / partial / full ...): its label says the same in words, the state is what the
// tests and the tone follow.
const (
	StateInstalled  = "installed"
	StateMissing    = "missing"
	StateInstalling = "installing"
	StateAttention  = "attention"
)

// Row is a line of the list: the software, what it is for, the page and section it is installed in, and how it stands.
type Row struct {
	Tool    string
	Name    string
	Purpose string
	// Href is where it is installed (a section of this page, of another category, or of the settings for Codex).
	Href string
	// State is the data-state; Label says it in words.
	State string
	Label string
}

// Index is the list of the companion software, with when it was read (beside the refresh button).
type Index struct {
	Rows      []Row
	UpdatedAt string
}

// Data is what the sections of the page draw. A nil FFmpeg or FaceFusion leaves that section out (this build has no such service).
type Data struct {
	Index      Index
	FFmpeg     *ffmpeg.Data
	FaceFusion *facefusion.Data
}
