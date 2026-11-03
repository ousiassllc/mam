package gentest

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
)

// ModelResultsID is the id of the part of the models that a changed filter draws again.
const ModelResultsID = "gt-model-results"

// modelCardOf is a model of the form as the card every list of models uses (molecules/modelcard.PickItem): a checkbox card. The data attributes are what the scripts of
// the input images and the detailed settings read (inputs-models.ts, advanced.ts, mam-prompt-weight).
func modelCardOf(m ModelChoice) modelcard.Card {
	card := m.Card
	card.ID = "gt-model-" + m.ID
	card.Pick = &modelcard.Pick{Mode: modelcard.PickMany, Name: FieldModel, Value: m.ID, Checked: m.Selected}
	if m.Reason != "" && card.Reason == nil {
		card.Reason = &modelcard.Reason{Text: m.Reason} // a model that is only used from another screen (the partial redraw)
	}
	card.Attrs = templ.Attributes{
		"data-testid":               "gt-model",
		"data-model":                m.ID,
		"data-label":                m.Label,
		"data-accepts-references":   boolText(len(m.RefRoles) > 0),
		"data-reference-mode":       m.RefMode,
		"data-reference-roles":      strings.Join(m.RefRoles, " "),
		"data-supports-lora":        boolText(m.CanLoRA),
		"data-requires-first-frame": boolText(m.RequiresFirstFrame),
		"data-sampling":             m.Use.Names(),
		"data-sampling-locked":      boolText(m.Use.Locked),
		"data-supports-seed":        boolText(m.Use.Seed),
		"data-default-steps":        strconv.FormatFloat(m.Use.StepsDefault, 'f', -1, 64),
		"data-min-steps":            strconv.Itoa(max(m.Use.StepsMin, 1)),
		"data-default-cfg":          strconv.FormatFloat(m.Use.CFGDefault, 'f', -1, 64),
		"data-prompt-weight":        m.Weight,
		"data-prompt-weight-text":   m.WeightText,
	}
	return card
}
