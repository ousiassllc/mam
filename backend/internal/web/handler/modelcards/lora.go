package modelcards

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/lorafamily"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// The keys of what a LoRA card sets in the field of the generation test's LoRA choice (modelpick.Input.Key). "value" is the file name, the field that is posted; the others
// are what the note under the field reads (static/src/components/gentest-lora): they are not posted.
const (
	LoRAKeyKnown        = "known"         // "true" for a file the user added through the model manager
	LoRAKeyFamilies     = "families"      // the families of base model it is made for (lorafamily keys, a space apart)
	LoRAKeyFamilyLabels = "family_labels" // their words
	LoRAKeyStrength     = "strength"      // the recommended strength ("" when not set)
	LoRAKeyTriggers     = "triggers"      // the trigger words, a comma apart
	LoRAKeyCommercial   = "commercial"    // the answer to "commercial use?" (yes / conditional / no / unknown)
)

// loraKeys are the keys of a LoRA field's inputs, "value" first (the change of the field is sent from the first input).
var loraKeys = []string{"value", LoRAKeyKnown, LoRAKeyFamilies, LoRAKeyFamilyLabels, LoRAKeyStrength, LoRAKeyTriggers, LoRAKeyCommercial}

// LoRAValues is what choosing the LoRA sets in the field; a nil choice is "なし" (every key empty).
func LoRAValues(c *loras.Choice) map[string]string {
	values := make(map[string]string, len(loraKeys))
	for _, k := range loraKeys {
		values[k] = ""
	}
	if c == nil {
		return values
	}
	values["value"], values[LoRAKeyKnown] = c.Name, strconv.FormatBool(c.Meta != nil)
	values[LoRAKeyCommercial] = commercial.FromInfo(c.License()).Answer()
	if m := c.Meta; m != nil {
		values[LoRAKeyFamilies], values[LoRAKeyFamilyLabels] = strings.Join(m.BaseModels, " "), strings.Join(lorafamily.Labels(m.BaseModels), "、")
		values[LoRAKeyTriggers] = strings.Join(m.TriggerWords, ", ")
		if m.Strength != nil {
			values[LoRAKeyStrength] = strconv.FormatFloat(*m.Strength, 'f', -1, 64)
		}
	}
	return values
}

// LoRAInputs are the inputs of the LoRA field with the values of the choice now (nil: なし). Only the first is posted (as name); the rest have no name.
func LoRAInputs(name string, c *loras.Choice) []modelpick.Input {
	values := LoRAValues(c)
	inputs := make([]modelpick.Input, 0, len(loraKeys))
	for _, k := range loraKeys {
		in := modelpick.Input{Key: k, Value: values[k]}
		if k == "value" {
			in.Name = name
		}
		inputs = append(inputs, in)
	}
	return inputs
}

// NoLoRA is the card of the choice "なし": the field says so and the picker lists it first.
func NoLoRA() modelcard.Card {
	return Choose(modelcard.Card{ID: "mp-lora-none", Name: "LoRA を使わない", Icon: atoms.IconImage, Meta: "なし"}, LoRAValues(nil))
}

// FromLoRA is the card of a LoRA file of the engine: its name, the families of base model it is made for and the recommended strength, the commercial-use icon
// (what the user confirmed; unknown for a file they did not add) and that it is installed. The trigger words and the license are in the note under the field once it is chosen.
func FromLoRA(c loras.Choice) modelcard.Card {
	answer := commercial.FromInfo(c.License())
	meta := "ベースモデル未設定"
	if c.Meta == nil {
		meta = "モデルマネージャーに登録していないファイル"
	} else if labels := lorafamily.Labels(c.Meta.BaseModels); len(labels) > 0 {
		meta = "ベース: " + strings.Join(labels, "、")
	}
	if c.Meta != nil && c.Meta.Strength != nil {
		meta += " ・ 推奨の強さ " + strconv.FormatFloat(*c.Meta.Strength, 'f', -1, 64)
	}
	card := modelcard.Card{
		ID: loraCardID(c.Name), Name: c.Name, Icon: atoms.IconImage, Meta: meta, Commercial: &answer, Note: answer.ShortNote(),
		Badges: modelcard.Facts{Installed: true}.Badges(),
	}
	return Choose(card, LoRAValues(&c))
}

// loraCardID is the id of the card of a LoRA file: the name has characters an id cannot take, so it is hashed.
func loraCardID(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return fmt.Sprintf("mp-lora-%08x", h.Sum32())
}
