// Package loraselect is the choice of a LoRA in the form of the generation test and in the recipes' form (#1119): the select of the LoRA files of the
// engine, and the note under it that says what is known of the chosen file (the base model it is made for, the trigger words, the license) and warns when
// it does not fit the model that is ticked. The note is drawn by static/src/components/gentest-lora/lora-note.ts from the data attributes of the options and of
// the model cards, so that it follows the choice without a request.
package loraselect

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
)

// Option is a LoRA file of the engine. Known is false for a file the model manager did not add (put in the folder by hand, or a catalog model's):
// nothing is known of it, and the other fields are empty.
type Option struct {
	Name  string
	Known bool
	// Families are the families of base model it is made for (lorafamily keys); none: not set.
	Families []string
	// Strength is the recommended strength ("" when not set).
	Strength string
	// Triggers are the trigger words (none: not set).
	Triggers []string
	// License is the license as the user confirmed it (the unknown answer when they gave none).
	License commercial.View
}

// Label is the text of the option: the file name and, for a file the user added, the answer to "commercial use?" (the same words as the model lists).
func (o Option) Label() string {
	if !o.Known {
		return o.Name
	}
	return commercial.OptionLabel(o.Name, o.License)
}

// Attrs are the data attributes the note reads.
func (o Option) Attrs() templ.Attributes {
	return templ.Attributes{
		"data-known": strconv.FormatBool(o.Known), "data-families": strings.Join(o.Families, " "), "data-family-labels": strings.Join(lorafamily.Labels(o.Families), "、"),
		"data-strength": o.Strength, "data-triggers": strings.Join(o.Triggers, ", "), "data-commercial": o.License.Answer(),
	}
}
