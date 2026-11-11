package charref_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/charref"
)

var project = domain.Project{ID: "p1"}

type fakeWorld struct {
	chars      []charref.Character
	added      []string // asset ids registered
	addedNames []string
	removed    []string
	assigned   []string
	addErr     error
	assignErr  error
}

func (f *fakeWorld) deps() charref.Deps {
	return charref.Deps{
		Characters: func(context.Context, string) ([]charref.Character, error) { return f.chars, nil },
		AddReference: func(_ context.Context, _, assetID, name string) (string, error) {
			if f.addErr != nil {
				return "", f.addErr
			}
			f.added, f.addedNames = append(f.added, assetID), append(f.addedNames, name)
			return "ref-" + assetID, nil
		},
		RemoveReference: func(_ context.Context, _, id string) error { f.removed = append(f.removed, id); return nil },
		Assign: func(_ context.Context, _, characterID, slot, referenceID string) error {
			if f.assignErr != nil {
				return f.assignErr
			}
			f.assigned = append(f.assigned, characterID+"/"+slot+"/"+referenceID)
			return nil
		},
	}
}

func chars(names ...string) []charref.Character {
	var out []charref.Character
	for _, n := range names {
		out = append(out, charref.Character{ID: "c-" + n, Name: n, Has: map[string]bool{}, Locked: map[string]bool{}})
	}
	return out
}

func withImage(id string) context.Context {
	return chatops.WithAttachments(context.Background(), []domain.Asset{{ID: id, ProjectID: "p1", Type: domain.AssetImage}})
}

func ask(t *testing.T, a charref.Args) commandinterpreter.Operation {
	t.Helper()
	op, err := commandinterpreter.NewOperation(charref.OpAssign, a)
	require.NoError(t, err)
	return op
}

func argsOf(t *testing.T, op commandinterpreter.Operation) charref.Args {
	t.Helper()
	var a charref.Args
	require.NoError(t, op.DecodeArgs(&a))
	return a
}

func TestQuestion_TheCardNamesTheImageTheCharacterAndTheScopeAndChangesNothing(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ", "ケン")}

	card, err := charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ", Slot: "face"}))

	require.NoError(t, err)
	assert.Contains(t, card.Text, "この画像を「ミオ」の顔（face）の参照にします")
	assert.Contains(t, card.Text, "Reference Library")
	assert.Contains(t, card.Text, "この Project の Character だけが変わります")
	assert.Contains(t, card.Text, "ほかのプロジェクトには影響しません")
	assert.NotContains(t, card.Text, "全プロジェクト")
	assert.Empty(t, f.added)
	assert.Empty(t, f.assigned)
	require.NotNil(t, card.Op, "the yes button carries the asset: its answer has no attachment")
	assert.Equal(t, charref.Args{Character: "ミオ", CharacterID: "c-ミオ", Slot: "face", AssetID: "a1"}, argsOf(t, *card.Op))
	body, err := chatops.Body(ask(t, charref.Args{}), card)
	require.NoError(t, err)
	assert.Equal(t, []string{chatops.Yes, chatops.No}, body.Options)
	var carried charref.Args
	op := commandinterpreter.Operation{}
	require.NoError(t, json.Unmarshal(body.OptionActions[0], &op))
	require.NoError(t, op.DecodeArgs(&carried))
	assert.Equal(t, "a1", carried.AssetID)
}

func TestQuestion_ReplacingAnImageSaysSo(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}
	f.chars[0].Has["body"] = true

	card, err := charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ", Slot: "body"}))

	require.NoError(t, err)
	assert.Contains(t, card.Text, "体（body）")
	assert.Contains(t, card.Text, "いま body 枠にある画像は外れます")
}

func TestQuestion_TheOnlyCharacterIsTakenWhenNoneIsNamed(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}
	for _, name := range []string{"", "主人公"} {
		card, err := charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: name, Slot: "face"}))

		require.NoError(t, err, name)
		assert.Equal(t, "c-ミオ", argsOf(t, *card.Op).CharacterID, name)
	}
}

func TestQuestion_SeveralCharactersAskWhichWithOneButtonEach(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ", "ミオの母", "ケン")}

	card, err := charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ", Slot: "face"}))
	require.NoError(t, err)
	assert.Empty(t, card.Choices, "ミオ is an exact name: only it matches")
	assert.Equal(t, "c-ミオ", argsOf(t, *card.Op).CharacterID)

	f.chars = chars("ミオさん", "ミオの母", "ケン")
	card, err = charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ", Slot: "face"}))
	require.NoError(t, err)
	require.Len(t, card.Choices, 2, "ミオ is part of both names")
	assert.Equal(t, "ミオさん の顔にする", card.Choices[0].Label)
	assert.Equal(t, "c-ミオの母", argsOf(t, card.Choices[1].Op).CharacterID)
	assert.Equal(t, "a1", argsOf(t, card.Choices[1].Op).AssetID)
	assert.Contains(t, card.Text, "どの Character")
	assert.Empty(t, f.assigned, "asking assigns nothing")

	f.chars = chars("ミオ", "ケン")
	card, err = charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Slot: "face"}))
	require.NoError(t, err)
	assert.Len(t, card.Choices, 2, "no name and several Characters: asks which")
}

func TestQuestion_AnUnknownCharacterIsRefusedWithTheNames(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ", "ケン")}

	_, err := charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: "ユウ", Slot: "face"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "「ユウ」という Character は見つかりません")
	assert.Contains(t, ref.Message, "「ミオ」、「ケン」")
	assert.Empty(t, f.added)
}

func TestQuestion_NoCharacterYetIsRefused(t *testing.T) {
	_, err := charref.New((&fakeWorld{}).deps()).Question(withImage("a1"), project, ask(t, charref.Args{Slot: "face"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "Character がいません")
}

func TestQuestion_WithoutAnImageTheChatSaysItMustBeAttached(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}
	video := chatops.WithAttachments(context.Background(), []domain.Asset{{ID: "v1", Type: domain.AssetVideo}})
	two := chatops.WithAttachments(context.Background(), []domain.Asset{{ID: "a1", Type: domain.AssetImage}, {ID: "a2", Type: domain.AssetImage}})

	for name, ctx := range map[string]context.Context{"none": context.Background(), "video": video, "two": two} {
		_, err := charref.New(f.deps()).Question(ctx, project, ask(t, charref.Args{Character: "ミオ", Slot: "face"}))

		var ref *chatops.Refusal
		require.ErrorAs(t, err, &ref, name)
		assert.Contains(t, ref.Message, "添付", name)
	}
}

func TestQuestion_ASlotLockedIsRefusedWithTheScreensReason(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}
	f.chars[0].Locked["face"] = true
	h := charref.New(f.deps())

	_, err := h.Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ", Slot: "face"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "LOCK されているため変更できません")
	_, err = h.Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ", Slot: "body"}))
	assert.NoError(t, err, "the other slot is free in this fake")
}

func TestQuestion_ALockedCharacterIsNotOfferedAmongSeveral(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ", "ケン")}
	f.chars[0].Locked["face"] = true

	card, err := charref.New(f.deps()).Question(withImage("a1"), project, ask(t, charref.Args{Slot: "face"}))

	require.NoError(t, err)
	assert.Empty(t, card.Choices)
	assert.Equal(t, "c-ケン", argsOf(t, *card.Op).CharacterID)
}

func TestQuestion_WithoutASlotAsksWhichOne(t *testing.T) {
	_, err := charref.New((&fakeWorld{chars: chars("ミオ")}).deps()).Question(withImage("a1"), project, ask(t, charref.Args{Character: "ミオ"}))

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "顔（face）と体（body）")
}

func TestQuestion_TheModelsOwnAssetIsNeverTrusted(t *testing.T) {
	f := &fakeWorld{chars: chars("ミオ")}

	card, err := charref.New(f.deps()).Question(withImage("real"), project, ask(t, charref.Args{Character: "ミオ", Slot: "face", AssetID: "invented", CharacterID: "c-other"}))

	require.NoError(t, err)
	assert.Equal(t, charref.Args{Character: "ミオ", CharacterID: "c-ミオ", Slot: "face", AssetID: "real"}, argsOf(t, *card.Op))
}

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
