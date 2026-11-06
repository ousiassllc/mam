// Package runtimecard is the section "ローカル音声・口パク" of the model manager screen: a card for every local runtime (GPT-SoVITS, MuseTalk) with its
// state, install, start and stop, and the list of its weights with their source and license (spec/components/model-manager-screen.md). It
// is a package of its own because the modelmgr package is at its size limit.
package runtimecard

import (
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/weightlist"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// RuntimeID is the id of the card of a local runtime (the element the actions of the card swap).
func RuntimeID(id string) string { return "mm-runtime-" + id }

// RuntimesID is the section of the local voice and lip-sync runtimes, LLMRuntimesID the one of the runtimes of a language model (llama.cpp, Ollama; the page "ローカル LLM", #1104).
const (
	RuntimesID    = "mm-runtimes"
	LLMRuntimesID = "mm-llm-runtimes"
)

// PurposeLLM is the value of the query "purpose" (GET /model-manager/runtimes?purpose=llm) and of Runtime.PurposeID for a runtime that makes text.
const PurposeLLM = "llm"

// OllamaID is the runtime of the Ollama of mam (the catalog's id): its card says what its removal takes along, and its detail that the server has no authentication.
const OllamaID = "ollama"

// Runtime is the card of a local runtime (GPT-SoVITS, MuseTalk): the same surface as the card of the engine.
type Runtime struct {
	view.Message
	ID      string
	Name    string
	Purpose string
	// PurposeID is what the runtime is for as the catalog says it ("tts", "lip_sync", "llm"): the card of an "llm" runtime is on the page "ローカル LLM", the others on "ローカル音声・口パク".
	PurposeID string
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
	// Models are the models of a runtime that has several (empty for one that has not); ModelsMessage is the answer to the last choice.
	Models        []Model
	ModelsMessage view.Message
	// ModelFilter is the filter of the list of models (#1011; the models in Models are the ones that pass it): its ID is empty when the list has no filter.
	ModelFilter modelpick.Filter
	// Commercial is the strictest answer of the weights and dependencies: the icon of the card (the list is in its detail).
	Commercial commercial.View
}

// Polling reports whether the card has something in progress (an install, a start).
func (r Runtime) Polling() bool { return r.Install != nil || r.State == "starting" }

// Runtimes is the section "ローカル音声・口パク", or with LLM the section "ローカルで動かすサーバー" of the page "ローカル LLM": the cards are those of the other purposes.
type Runtimes struct {
	view.Message
	Items []Runtime
	LLM   bool
}

// sectionID is the id of the section.
func (r Runtimes) sectionID() string {
	if r.LLM {
		return LLMRuntimesID
	}
	return RuntimesID
}

// sectionURL is what the refresh button of the section reads.
func (r Runtimes) sectionURL() string {
	if r.LLM {
		return paths.ModelManagerPart("runtimes") + "?purpose=" + PurposeLLM
	}
	return paths.ModelManagerPart("runtimes")
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
