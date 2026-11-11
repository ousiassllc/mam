package loras_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
)

func choices() []loras.Choice {
	s := 0.7
	return []loras.Choice{
		{Name: "mine.safetensors", Meta: &loras.Meta{BaseModels: []string{"sdxl"}, Strength: &s, TriggerWords: []string{"mychar"}, License: modellicense.FromUser("Mine", modellicense.Conditional)}},
		{Name: "no.safetensors", Meta: &loras.Meta{License: modellicense.FromUser("", modellicense.No)}},
		{Name: "bare.safetensors", Meta: &loras.Meta{License: modellicense.FromUser("", "")}},
		{Name: "by-hand.safetensors"},
	}
}

// The item of a LoRA says only what is known of it: it is local, installed and can be chosen; a file nobody recorded, or recorded without a license, is "unknown"
// for commercial use (never OK); nothing is claimed of its rating, size, VRAM or kind, so no filter of those matches it as a value.
func TestAnItemSaysOnlyWhatIsKnownOfTheLoRA(t *testing.T) {
	got := map[string]modelfilter.Item{}
	for _, c := range choices() {
		got[c.Name] = c.Item()
	}
	mine := got["mine.safetensors"]
	assert.Equal(t, modellicense.Conditional, mine.Commercial)
	assert.Equal(t, modellicense.No, got["no.safetensors"].Commercial)
	assert.Equal(t, modellicense.Unknown, got["bare.safetensors"].Commercial)
	assert.Equal(t, modellicense.Unknown, got["by-hand.safetensors"].Commercial)
	for name, it := range got {
		assert.Equal(t, name, it.ID)
		assert.True(t, it.Installed, name)
		assert.True(t, it.Available, name)
		assert.Equal(t, modelfilter.OriginLocal, it.Origin, name)
		assert.False(t, it.Adult || it.Uncensored, "%s: no rating is claimed", name)
		assert.Zero(t, it.ParamsB, name)
		assert.Zero(t, it.VRAMMinGB, name)
		assert.Empty(t, it.Kinds, name)
		assert.Empty(t, it.Inputs, name)
	}
}

func names(list []loras.Choice) []string {
	out := []string{}
	for _, c := range list {
		out = append(out, c.Name)
	}
	return out
}

// The filter of the picker narrows the LoRAs by what the user confirmed (commercial use) and by the search, which reads the name, the base model (its key and its
// words) and the trigger words.
func TestTheFilterNarrowsTheLoRAs(t *testing.T) {
	narrow := func(c modelfilter.Condition) []string {
		return names(modelfilter.Filter(c, choices(), loras.Choice.Item))
	}

	assert.Len(t, narrow(modelfilter.Default()), 4, "the start shows every file: all can be chosen")
	assert.Equal(t, []string{"mine.safetensors"}, narrow(modelfilter.Condition{Commercial: "conditional"}))
	assert.Equal(t, []string{"mine.safetensors", "no.safetensors", "bare.safetensors", "by-hand.safetensors"}, narrow(modelfilter.Condition{Commercial: "all"}))
	assert.Equal(t, []string{"no.safetensors"}, narrow(modelfilter.Condition{Commercial: "ng"}))
	assert.Equal(t, []string{"bare.safetensors", "by-hand.safetensors"}, narrow(modelfilter.Condition{Commercial: "unknown"}))
	assert.Equal(t, []string{"mine.safetensors"}, narrow(modelfilter.Condition{Text: "mychar"}), "trigger word")
	assert.Equal(t, []string{"mine.safetensors"}, narrow(modelfilter.Condition{Text: "sdxl"}), "base model")
	assert.Equal(t, []string{"by-hand.safetensors"}, narrow(modelfilter.Condition{Text: "BY-HAND"}), "name, any case")
	assert.Empty(t, narrow(modelfilter.Condition{Params: []modelfilter.Band{modelfilter.BandSmall}}), "no size is known: a size filter matches none")
}
