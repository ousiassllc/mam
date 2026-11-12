package charref_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/charref"
)

func TestApply_RegistersTheImageAndAssignsItOnce(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}

	res, err := charref.New(f.deps()).Apply(context.Background(), project, ask(t, charref.Args{Character: "ミオ", CharacterID: "c-ミオ", Slot: "face", AssetID: "a1"}))

	require.NoError(t, err)
	assert.Equal(t, []string{"a1"}, f.added)
	assert.Equal(t, []string{"ミオ（顔）"}, f.addedNames)
	assert.Equal(t, []string{"c-ミオ/face/ref-a1"}, f.assigned)
	assert.Empty(t, f.removed)
	assert.Contains(t, res.Text, "「ミオ」の顔（face）の参照を設定しました")
	assert.Contains(t, res.Text, "この Project の Character だけ")
}

func TestApply_ALockSetSinceTheCardRefusesAndRegistersNothing(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}
	f.chars[0].Locked["face"] = true

	_, err := charref.New(f.deps()).Apply(context.Background(), project, ask(t, charref.Args{CharacterID: "c-ミオ", Slot: "face", AssetID: "a1"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Empty(t, f.added)
	assert.Empty(t, f.assigned)
}

func TestApply_ARefusedAssignmentTakesTheNewLibraryEntryOutAgain(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ"), assignErr: chatops.Refuse("LOCK されている項目は変更できません")}

	_, err := charref.New(f.deps()).Apply(context.Background(), project, ask(t, charref.Args{CharacterID: "c-ミオ", Slot: "face", AssetID: "a1"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Equal(t, []string{"ref-a1"}, f.removed, "nothing is left behind")
	assert.Empty(t, f.assigned)
}

func TestApply_ARefusedImageAssignsNothing(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ"), addErr: chatops.Refuse("画像（jpg/png/webp/gif）を選んでください")}

	_, err := charref.New(f.deps()).Apply(context.Background(), project, ask(t, charref.Args{CharacterID: "c-ミオ", Slot: "face", AssetID: "a1"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Empty(t, f.assigned)
	f.addErr = errors.New("disk full")
	_, err = charref.New(f.deps()).Apply(context.Background(), project, ask(t, charref.Args{CharacterID: "c-ミオ", Slot: "face", AssetID: "a1"}))
	assert.EqualError(t, err, "disk full")
}

func TestApply_AnIncompleteOrUnknownOperationIsRefused(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}
	h := charref.New(f.deps())

	for _, a := range []charref.Args{
		{Slot: "face", AssetID: "a1"},                        // no character
		{CharacterID: "c-ミオ", Slot: "face"},                  // no asset
		{CharacterID: "c-ミオ", AssetID: "a1"},                 // no slot
		{CharacterID: "c-gone", Slot: "face", AssetID: "a1"}, // the Character is gone
	} {
		_, err := h.Apply(context.Background(), project, ask(t, a))
		var ref *chatops.Refusal
		require.ErrorAs(t, err, &ref, a)
	}
	assert.Empty(t, f.added)
}

func TestLibraryName_IsCutToWhatTheLibraryTakes(t *testing.T) {
	long := strings.Repeat("あ", 300)
	f := &fakeWorld{chars: chars(long)}

	_, err := charref.New(f.deps()).Apply(context.Background(), project, ask(t, charref.Args{CharacterID: "c-" + long, Slot: "body", AssetID: "a1"}))

	require.NoError(t, err)
	require.Len(t, f.addedNames, 1)
	assert.LessOrEqual(t, len([]rune(f.addedNames[0])), domain.MaxReferenceNameRunes)
	assert.True(t, strings.HasSuffix(f.addedNames[0], "（体）"))
}
