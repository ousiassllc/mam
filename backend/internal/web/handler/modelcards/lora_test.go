package modelcards_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
)

// Choosing the card of a LoRA sets the file name and what the note under the field reads; "なし" sets every key empty; a file nobody recorded is not "known".
func TestTheCardOfALoRASetsTheFileAndWhatTheNoteReads(t *testing.T) {
	s := 0.7
	mine := loras.Choice{Name: "mine.safetensors", Meta: &loras.Meta{
		BaseModels: []string{"sdxl", "flux"}, Strength: &s, TriggerWords: []string{"mychar", "red dress"}, License: modellicense.FromUser("Mine", modellicense.Conditional),
	}}
	card := modelcards.FromLoRA(mine)
	assert.Equal(t, modelcard.PickOne, card.Pick.Mode)
	assert.Equal(t, map[string]string{
		"value": "mine.safetensors", "known": "true", "families": "sdxl flux", "family_labels": "SDXL（Pony・Illustrious を含む）、FLUX.1", "strength": "0.7",
		"triggers": "mychar, red dress", "commercial": "conditional",
	}, card.Pick.Values)
	assert.Contains(t, card.Meta, "SDXL")
	assert.Contains(t, card.Meta, "0.7")
	assert.NotNil(t, card.Commercial)
	var labels []string
	for _, b := range card.Badges {
		labels = append(labels, b.Label)
	}
	assert.Equal(t, []string{"導入済み"}, labels, "installed is all a LoRA has of the shared badges: no rating, size or VRAM")

	byHand := modelcards.FromLoRA(loras.Choice{Name: "by-hand.safetensors"}).Pick.Values
	assert.Equal(t, "false", byHand["known"])
	assert.Equal(t, "unknown", byHand["commercial"])
	assert.Empty(t, byHand["families"])

	none := modelcards.NoLoRA().Pick.Values
	assert.Len(t, none, 7)
	for k, v := range none {
		assert.Empty(t, v, k)
	}
}

// The field posts only the file name; the other inputs hold what the note reads and have no name.
func TestOnlyTheFileOfTheLoRAFieldHasAName(t *testing.T) {
	inputs := modelcards.LoRAInputs("lora", &loras.Choice{Name: "a.safetensors"})
	assert.Equal(t, "value", inputs[0].Key, "the change of the field is sent from the file's input")
	for _, in := range inputs {
		assert.Equal(t, in.Key == "value", in.Name == "lora", in.Key)
	}
	assert.Equal(t, "a.safetensors", inputs[0].Value)
}

func TestTheCardIdOfALoRAIsAValidIdWhateverItsName(t *testing.T) {
	a := modelcards.FromLoRA(loras.Choice{Name: "a b/(c).safetensors"}).ID
	b := modelcards.FromLoRA(loras.Choice{Name: "other.safetensors"}).ID
	assert.Regexp(t, `^mp-lora-[0-9a-f]{8}$`, a)
	assert.NotEqual(t, a, b)
}
