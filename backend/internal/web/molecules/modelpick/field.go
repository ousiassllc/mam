package modelpick

import (
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
)

// The ids the script and the handlers share.
const (
	// DialogID is the dialog of the picker, one per page (Host); BodyID is the element the answer of the request that opens it goes into; TitleID its title.
	DialogID = "mam-model-picker"
	BodyID   = "mam-model-picker-body"
	TitleID  = "mam-model-picker-title"
	// FailureID is where a request of the picker that got no answer says so.
	FailureID = "mam-model-picker-failure"
	// KeyCurrentPrefix is the prefix of the parameters that carry what the field holds when the picker is opened (cur_value, cur_provider): the picker marks the
	// card of the model that is chosen now.
	KeyCurrentPrefix = "cur_"
)

// Input is a hidden input of a field: the name the form posts, the key of the value in what a card sets (modelcard.Pick.Values; "value" for the main one) and the
// value held now. An input with no Name is not posted: it holds what the script of the screen reads of the choice (the LoRA note's base model).
type Input struct{ Name, Key, Value string }

// FieldData is what the control that chooses one model (Field) is drawn from: it shows the model chosen (the same summary as its card) and opens the picker when pressed. The value is in hidden
// inputs, so the form that holds the field posts it like a select (the LLM's pair of provider and model is two inputs).
type FieldData struct {
	// ID is the id of the field (its button is ID+"-button").
	ID string
	// Label is the text the button is named by; LabelID the id of the visible label that names it ("" when Label alone does).
	Label string
	// Inputs are the hidden inputs the choice sets.
	Inputs []Input
	// Current is what the field shows. A Name of "" shows the invitation to choose.
	Current modelcard.Card
	// URL is the address of the picker (GET), which answers PickerBody for the field.
	URL string
	// Disabled makes the field unusable and keeps it out of the form (the control it belongs to is not offered now).
	Disabled bool
	// Invalid marks the field a refusal is about (aria-invalid, described by DescribedBy) and takes the focus (Autofocus).
	Invalid, Autofocus bool
	DescribedBy        string
	// Note is the line under the field in the warning colour (the saved model cannot be used now).
	Note string
	// TestID names the field for the tests.
	TestID string
}

// buttonID is the id of the button of the field.
func (f FieldData) buttonID() string { return f.ID + "-button" }

// Picker is the body of the dialog: its title, the bar and the results (the cards).
type Picker struct {
	Title string
	Bar   Filter
	// ResultsID is the id of the results the filter asks to be drawn again.
	ResultsID string
	Cards     []modelcard.Card
	// Empty is what is said when no model passes the filters.
	Empty string
}

// fieldBorder is the border of the field's button: red for the field a refusal is about, else the border of the form fields.
func fieldBorder(invalid bool) string {
	if invalid {
		return "border-danger/60"
	}
	return "border-mute"
}
