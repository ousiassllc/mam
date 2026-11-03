package modelmgr

import (
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// HFToken is the optional Hugging Face token: whether there is one (and its end), never its value.
type HFToken struct {
	Message
	Configured bool
	LastChars  string
	// Unavailable: the key store cannot be read.
	Unavailable bool
}

// ExternalModel is a catalog model against an external ComfyUI.
type ExternalModel struct {
	Name     string
	Complete bool
	Present  []string
	Missing  []string
}

// ExternalFolder is a model folder of an external ComfyUI with its files.
type ExternalFolder struct {
	Name  string
	Files []string
}

// ExternalResult is what an external ComfyUI has.
type ExternalResult struct {
	Endpoint string
	Version  string
	Folders  []ExternalFolder
	Models   []ExternalModel
}

// External is the view of an external ComfyUI: the endpoints to choose from and, once asked, the result.
type External struct {
	Message
	Endpoints []string
	Selected  string
	Result    *ExternalResult
}

// TrialForm is the form that starts a trial generation, for the model that is chosen.
type TrialForm struct {
	Message
	// Model is the field that chooses the model: the model picker (#1011), the same cards and filter as everywhere. ModelID is the model chosen ("" while there is none) and
	// HasModels says there is some to choose from.
	Model     modelpick.FieldData
	ModelID   string
	HasModels bool
	// Media is image or video ("" while no model is chosen).
	Media            string
	AcceptsReference bool
	Resolutions      []string
	Durations        []string
	// NativeAudio: the chosen video model makes the sound with the picture; the form says so.
	NativeAudio bool
	Prompt      string
	Negative    string
	Aspect      string
	Resolution  string
	MaxPrompt   int
	Duration    string
}

// Trial is a trial generation of the list.
type Trial struct {
	ID      string
	ModelID string
	Answer  commercial.View
	Video   bool
	// Audio: the model of the result makes the sound with the picture, so the file has an audio track.
	Audio      bool
	Prompt     string
	Detail     string
	Reference  bool
	Status     string
	Error      string
	CreatedAt  string
	Size       string
	FileURL    string
	Progress   *Progress
	Deletable  bool
	StatusText string
}

// Trials is the list of trials.
type Trials struct {
	Message
	Items []Trial
	// UpdatedAt is the time of day the list was read ("15:04:05"), beside its refresh button (#962).
	UpdatedAt string
}

// Polling reports whether a trial waits or runs.
func (t Trials) Polling() bool {
	for _, it := range t.Items {
		if it.Status == "running" || it.Status == "queued" {
			return true
		}
	}
	return false
}

// TrialPanel is the trial form and the results.
type TrialPanel struct {
	Form   TrialForm
	Trials Trials
	// OOB draws the section out of band (a model was installed or deleted: the choices changed).
	OOB bool
	// Unavailable says why no trial can start ("" when one can): no model is installed or registered.
	Unavailable string
}
