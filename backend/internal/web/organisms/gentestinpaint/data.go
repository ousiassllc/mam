// Package gentestinpaint draws the partial-redraw screen (paths.GenerationTestInpaint, #974).
package gentestinpaint

import "github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"

// Page is the form. MaxPrompt is the server's prompt limit (gentest.MaxPromptRunes), handed in by the handler.
type Page struct {
	Error, SourceURL, SourceRef, SourceLabel, Denoise string
	// Notice is the line a redo opens with ("この設定でもう一度"). Prompt, Negative, MaskURL and Seed are the settings of the result it takes (MaskURL is the mask
	// the painter starts from; Seed is "" unless the seed is kept).
	Notice, Prompt, Negative, MaskURL, Seed string
	// Model is the field that chooses the model: the model picker (#1011), which lists the models that can redraw part of a picture and says why the others cannot.
	Model               modelpick.FieldData
	MaxPrompt           int
	HasModel, Translate bool
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
