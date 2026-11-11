package charref_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/charref"
)

func TestDetect_WeightAndTypefaceAreReadAsTheBodySlotWhichTheCardThenAsksAbout(t *testing.T) {
	for message, want := range map[string]charref.Args{
		"この画像をミオの体重の参照にして": {Character: "ミオ", Slot: "body"},
		"この画像をミオの書体の参照にして": {Slot: "body"}, // the name is not read through 書体: the card asks which Character
	} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Equal(t, want, got[0], message)
	}
}

func TestDetect_ANameKeepsItsOwnCharactersAndOnlyTheParticleAfterTheImageWordGoes(t *testing.T) {
	for message, want := range map[string]string{
		"この画像をゆにの顔にして":     "ゆに",
		"この画像をももの顔にして":     "もも",
		"この画像をはるかの顔にして":    "はるか",
		"この画像はミオの顔にして":     "ミオ",
		"添付の写真はにこの顔の参照にして": "にこ",
		"この画像をミオの顔にして":     "ミオ",
	} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Equal(t, want, got[0].Character, message)
	}
}

func TestApply_AFailedAssignmentIsTheErrorTheUserHearsEvenWhenTheCleanupFailsToo(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}
	d := f.deps()
	assignErr := &chatops.Refusal{Message: "枠が使えません"}
	cleanupErr := errors.New("library down")
	d.Assign = func(context.Context, string, string, string, string) error { return assignErr }
	d.RemoveReference = func(context.Context, string, string) error { return cleanupErr }

	_, err := charref.New(d).Apply(context.Background(), project, ask(t, charref.Args{CharacterID: "c-ミオ", Slot: "face", AssetID: "a1"}))

	var refusal *chatops.Refusal
	require.ErrorAs(t, err, &refusal, "the refusal of the assignment is not hidden by the failed cleanup")
	assert.Equal(t, "枠が使えません", refusal.Message)
	assert.ErrorIs(t, err, cleanupErr, "the entry left in the Library is not lost either")
}
