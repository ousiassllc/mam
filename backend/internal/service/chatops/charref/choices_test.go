package charref_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/charref"
)

// More Characters than a card holds as buttons are asked to be named, with no button and nothing assigned.
func TestQuestion_TooManyCharactersAreAskedToBeNamed(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ1", "ミオ2", "ミオ3", "ミオ4", "ミオ5", "ミオ6", "ミオ7")}

	card, err := charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ", Slot: "face"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "多すぎ")
	assert.Contains(t, ref.Message, "「ミオ1」")
	assert.NotContains(t, ref.Message, "「ミオ7」", "only the first few are listed")
	assert.Empty(t, card.Choices)
	assert.Empty(t, f.assigned)
}
