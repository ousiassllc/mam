// Package runtimecard is the section "ローカル音声・口パク" of the model manager screen: a card for every local runtime (GPT-SoVITS, MuseTalk) with its
// state, install, start and stop, and the list of its weights with their source and license (spec/components/model-manager-screen.md). It
// is a package of its own because the modelmgr package is at its size limit.
package runtimecard

import (
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/weightlist"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// RuntimeID is the id of the card of a local runtime (the element the actions of the card swap).
func RuntimeID(id string) string { return "mm-runtime-" + id }

// RuntimesID is the section of the local runtimes.
const RuntimesID = "mm-runtimes"

// Runtime is the card of a local runtime (GPT-SoVITS, MuseTalk): the same surface as the card of the engine.
type Runtime struct {
	view.Message
	ID      string
	Name    string
	Purpose string
	// Icon is the picture of the card (the tile of the purpose: a voice, a lip sync).
	Icon        atoms.IconName
	Description string
	SourceURL   string
	// Supported is false on a machine that cannot run the runtime; Reason says why (the same sentence on every screen) and Fallback
	// what to use instead (the cloud provider).
	Supported bool
	Reason    string
	Fallback  string
	GPU       string

	Writable      bool
	WritableError string
	Dir           string
	Free          string
	// Need is the free space an install needs; VRAM is the guideline of the video memory (with its source).
	Need string
	VRAM string

	Installed       bool
	Broken          bool
	Version         string
	PinnedVersion   string
	VariantLabel    string
	UpdateAvailable bool

	// State is stopped, starting, running or failed.
	State        string
	URL          string
	ProcessError string
	AutoStart    bool

	Install *view.Progress
	// Job is the install job the card reports on (running, or one that just ended).
	Job string

	LicenseName      string
	LicenseURL       string
	LicenseCondition string
	Weights          weightlist.Weights
	// Commercial is the strictest answer of the weights and dependencies: the icon of the card (the list is in its detail).
	Commercial commercial.View
}

// Polling reports whether the card has something in progress (an install, a start).
func (r Runtime) Polling() bool { return r.Install != nil || r.State == "starting" }

// Runtimes is the section "ローカル音声・口パク".
type Runtimes struct {
	view.Message
	Items []Runtime
}

// detailURL is what the card opens: the modal with the sources, licenses, files and SHA-256 of the runtime.
func detailURL(r Runtime) string { return paths.ModelManagerPart("runtimes", r.ID, "detail") }

// runtimeURL is where the card asks for itself again: with the job it reports on.
func runtimeURL(r Runtime) string {
	base := paths.ModelManagerPart("runtimes", r.ID)
	if r.Job == "" {
		return base
	}
	return base + "?job=" + url.QueryEscape(r.Job)
}
