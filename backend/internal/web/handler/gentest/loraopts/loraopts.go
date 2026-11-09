// Package loraopts makes the LoRA choice of the generation test form (#1119, #1011) from the LoRA files of the engine, with what the model manager knows of each:
// the field of the model picker (source "lora") that shows the LoRA chosen and opens the dialog of cards.
package loraopts

import (
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelpicker"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// FieldID is the id of the field; Label is what the button is named by.
const (
	FieldID = "gt-lora-name"
	Label   = "LoRA"
)

// Field is the field that chooses the LoRA posted as name: the file chosen now (selected; "" or a file that the engine does not list: なし), shown as the card of the
// picker shows it. It is nil when the engine lists no LoRA (there is nothing to choose). media is the context the picker lists for.
func Field(name, selected, media string, choices []loras.Choice) *modelpick.FieldData {
	if len(choices) == 0 {
		return nil
	}
	var chosen *loras.Choice
	for i := range choices {
		if choices[i].Name == selected {
			chosen = &choices[i]
		}
	}
	current := modelcards.NoLoRA()
	if chosen != nil {
		current = modelcards.FromLoRA(*chosen)
	}
	f := modelpicker.FieldFor(modelpicker.SourceLoRA, FieldID, Label, url.Values{modelpicker.QueryMedia: {media}}, modelcards.LoRAInputs(name, chosen), current)
	f.TestID = "gt-lora-field"
	return &f
}
