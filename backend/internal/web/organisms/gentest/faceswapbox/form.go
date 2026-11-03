// Package faceswapbox draws the "swap the face" box of the detail of a generation test result (spec/architecture/gentest/face-swap.md §3): the
// picture of the face to upload, the three FaceFusion models to choose (each with what its license allows, and a warning the moment one is
// chosen that is not for commercial use), the cautions that hold for every swap, and the button. It is a package of its own (like upscalebox)
// so that organisms/gentest stays under the line limit; the handlers build its Form.
package faceswapbox

// IDs of the box and of the part of it that the warnings replace.
const (
	ID         = "gt-faceswap"
	WarningsID = "gt-faceswap-warnings"
)

// The fields of the box's form (the handler reads the same names).
const (
	FieldFace     = "face"
	FieldSwapper  = "swapper"
	FieldDetector = "detector"
	FieldEnhancer = "enhancer"
)

// Choice is a model the box offers: its id, how it is shown (with the license it has) and whether it is selected.
type Choice struct {
	ID       string
	Label    string
	Selected bool
}

// Form is the box of the detail of a result. Show is false for a result that is no picture to swap into (a video, or one without a file). Problem
// is why a swap cannot start now (FaceFusion is not set up: the box then links to the settings instead of the form). Notice and Error are a
// green and a red line.
type Form struct {
	Notice, Error string
	ResultID      string
	Show          bool
	Problem       string
	SettingsURL   string
	Swappers      []Choice
	Detectors     []Choice
	Enhancers     []Choice
	// Warnings are the cautions of the models selected now.
	Warnings []string
	// Notes are the cautions that hold for every swap (FaceFusion's license, ArcFace, adult pictures, the models it fetches itself).
	Notes []string
	// SourceURL and SourceText link a result of a swap to the result it swapped into ("" for any other).
	SourceURL, SourceText string
	// WarningsURL is where the three selects ask for the warnings of the models they hold.
	WarningsURL string
}
