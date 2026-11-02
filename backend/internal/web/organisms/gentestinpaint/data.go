// Package gentestinpaint draws the partial-redraw screen (paths.GenerationTestInpaint, #974).
package gentestinpaint

// Model is one image model. On is false when it cannot redraw part of a picture; Reason says why.
type Model struct {
	ID, Label, Reason string
	Denoise           float64
	On, Pick          bool
}

// Page is the form.
type Page struct {
	Error, SourceURL, SourceRef, SourceLabel, Denoise string
	Models                                            []Model
	HasModel, Translate                               bool
}

// Field is a hidden value the confirmation posts back.
type Field struct{ Name, Value string }

// Box is the estimate the user confirms.
type Box struct {
	Items    int
	Total    string
	Warnings []string
	Over     string
	Hidden   []Field
}
