package runtimecard

// The fields of the form that chooses the model of a runtime: the model, and one acceptance per model whose license has to be accepted (its
// value is the version of the text the user was shown).
const (
	FieldModel        = "model"
	FieldAcceptPrefix = "accept_"
)

// ModelsID is the form of the models of a runtime (the element the choice swaps).
func ModelsID(id string) string { return "mm-runtime-models-" + id }

// ModelListID is the list of the models of a runtime with the line above it (the element the filter swaps); ModelBarID is the filter bar.
func ModelListID(id string) string { return "mm-runtime-model-list-" + id }

// ModelBarID is the id of the filter bar of the models of a runtime.
func ModelBarID(id string) string { return "mm-runtime-model-bar-" + id }

// hasModels says whether the runtime has several models to choose from: the models that pass the filter, or the ones it holds back.
func (r Runtime) hasModels() bool { return len(r.Models) > 0 || r.ModelFilter.Total > 0 }

// Model is one model of a runtime that has several.
type Model struct {
	ID, Name, Description string
	Active, Default       bool
	// Selected is the radio as the user left it (the model in force until they choose another; the filter draws the list again with their choice kept).
	Selected bool
	// Uncensored: the model's refusals were removed (an abliterated derivative); the screen says it is for adults.
	Uncensored      bool
	Size, Languages string
	// VRAM is the guideline of the video memory; VRAMFit says how this PC's GPU stands to it ("" when not known).
	VRAM, VRAMFit string
	License       string
	LicenseURL    string
	Commercial    string
	// Cautions are the restrictions of the use of the model, as its card states them.
	Cautions []string
	// Consent is the license to accept before the model is used (nil for a model with none).
	Consent *ModelConsent
}

// ModelConsent is the license a model asks the user to accept: the text of the version it shows. Ticked is the checkbox as the user left it before the
// list was drawn again by the filter (the acceptance is the user's own: nothing ticks it but the user).
type ModelConsent struct {
	Version, Text string
	Accepted      bool
	Ticked        bool
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
