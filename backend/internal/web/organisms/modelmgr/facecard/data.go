// Package facecard is the section "顔チェック用モデル" of the model manager screen (Issue #1026): the YuNet and SFace models that the face similarity of the
// generation test reads, with where they come from, their licenses and commercial-use answers, and the buttons that install them into the model folder with one
// click (the licenses and the source are right above the button, and the install needs the box ticked) and remove them. It is a package of its own because the
// modelmgr package is at its size limit.
package facecard

import (
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// IDs and fields of the section.
const (
	// SectionID is the section (the anchor the links of the generation test point to: /model-manager#mm-face-models).
	SectionID = "mm-face-models"
	// ProgressID is the line the install's progress is written to while it runs; InstallID the install button that starts its polling.
	ProgressID = "mm-face-models-progress"
	InstallID  = "mm-face-models-install"
	// FieldAccept is the box that has to be ticked to install.
	FieldAccept = "accept"
	acceptClass = "data-mm-face-accept"
)

// Anchor is where the screen is opened to find the section (the link "顔チェック用モデルを入れる").
func Anchor() string { return paths.ModelManagerFaceModels() }

// The states of the models (the same as facemodels.State).
const (
	StateInstalled  = "installed"
	StateUnverified = "unverified"
	StateMissing    = "missing"
)

// File is one model file of the section.
type File struct {
	Name        string
	Title       string
	Description string
	Size        string
	// State is installed / unverified / missing; StateText says it in words.
	State      string
	StateText  string
	SourceName string
	SourceURL  string
}

// License is a license of the models with the answer of whether it may be used commercially.
type License struct {
	Name    string
	Summary string
	// URL is the license's original. Notice is what to watch for, shown whatever the answer is (the weights' license says yes while the training data's rights are not stated).
	URL    string
	Notice string
	Answer commercial.View
}

// Card is the section: its state, the files, the licenses and what the last action said.
type Card struct {
	view.Message
	// State is installed / unverified / missing.
	State    string
	ModelDir string
	Files    []File
	Licenses []License
	// Download is how much an install downloads ("37.0 MiB"); Remain how much of it is still to fetch ("" unless some files are in place already).
	Download string
	Remain   string
	// OffReason is why the face similarity is off now ("": it works): "models_missing" or "runtime_unavailable" (the models are in place and the ONNX Runtime
	// library is what is missing: installing the models again does not fix that). OffWords is the cause and the fix in words.
	OffReason string
	OffCause  string
	OffFix    string
	// Running: an install is in progress (the page was opened or reloaded while it runs): the progress line polls from the start.
	Running bool
}

func progressURL() string { return paths.ModelManagerPart("face-models", "progress") }
func installURL() string  { return paths.ModelManagerPart("face-models", "install") }
func removeURL() string   { return paths.ModelManagerPart("face-models") }

// trigger is how the progress line asks again: a second after it was drawn while the install runs, else after the install button was pressed.
func trigger(running bool) string {
	if running {
		return "load delay:1s"
	}
	return "click from:#" + InstallID + " delay:500ms"
}

// HasFiles reports whether any model file is in the folder (there is something to remove).
func (c Card) HasFiles() bool {
	for _, f := range c.Files {
		if f.State != StateMissing {
			return true
		}
	}
	return false
}

// InstallLabel is the label of the install button.
func (c Card) InstallLabel() string {
	if c.State == StateUnverified {
		return "ファイルを確認する"
	}
	return "顔チェック用モデルを導入"
}
