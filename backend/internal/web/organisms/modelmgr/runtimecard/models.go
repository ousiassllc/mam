package runtimecard

// The fields of the form that chooses the model of a runtime: the model, and one acceptance per model whose license has to be accepted (its
// value is the version of the text the user was shown).
const (
	FieldModel        = "model"
	FieldAcceptPrefix = "accept_"
)

// ModelsID is the form of the models of a runtime (the element the choice swaps).
func ModelsID(id string) string { return "mm-runtime-models-" + id }

// Model is one model of a runtime that has several.
type Model struct {
	ID, Name, Description string
	Active, Default       bool
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

// ModelConsent is the license a model asks the user to accept: the text of the version it shows.
type ModelConsent struct {
	Version, Text string
	Accepted      bool
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
