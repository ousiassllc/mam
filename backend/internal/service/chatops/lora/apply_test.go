package lora_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/lora"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

func TestApply_TheYesAddsTheFileOnceWithWhatTheFileSaysOfItself(t *testing.T) {
	imp := newImporter()
	st := staged("tok1", "style.safetensors")
	st.Meta = lorafile.Metadata{BaseModels: []string{"sdxl", "flux"}, TriggerWords: []string{"wcstyle", "soft"}, License: "MIT"}
	imp.next["style.safetensors"] = st
	o := lora.New(imp)
	msgs, err := o.Offer(context.Background(), project, "", []chatops.File{file("style.safetensors", "x")})
	require.NoError(t, err)
	_, yes, _ := cardOf(t, msgs[0])

	res, err := o.Apply(context.Background(), project, yes)

	require.NoError(t, err)
	assert.Contains(t, res.Text, "「style.safetensors」を LoRA として追加しました")
	assert.Contains(t, res.Text, "全プロジェクト")
	require.Len(t, imp.commits, 1)
	assert.Equal(t, loraimport.Input{Token: "tok1", Meta: loraimport.Meta{BaseModels: []string{"sdxl", "flux"}, TriggerWords: "wcstyle, soft", LicenseName: "MIT"}}, imp.commits[0])
	assert.Nil(t, imp.commits[0].Strength, "the strength and the answer to commercial use are not invented")
	assert.Empty(t, imp.commits[0].Commercial)
	assert.Empty(t, imp.discards)

	_, err = o.Apply(context.Background(), project, yes)
	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref, "a file is added once")
	assert.Equal(t, "確認待ちのファイルが見つかりません。もう一度添付してください", ref.Message)
	assert.Len(t, imp.commits, 1)
}

func TestApply_DecliningThrowsTheFileAwayAndAddsNothing(t *testing.T) {
	imp := newImporter()
	imp.next["style.safetensors"] = staged("tok1", "style.safetensors")
	o := lora.New(imp)
	msgs, err := o.Offer(context.Background(), project, "", []chatops.File{file("style.safetensors", "x")})
	require.NoError(t, err)
	_, _, no := cardOf(t, msgs[0])

	res, err := o.Apply(context.Background(), project, no)

	require.NoError(t, err)
	assert.Equal(t, []string{"tok1"}, imp.discards)
	assert.Empty(t, imp.commits)
	assert.Empty(t, imp.waiting, "the file is gone from the imports folder")
	assert.Contains(t, res.Text, "何も追加していません")
}

func TestApply_AFileThatNoLongerWaitsIsToldToAttachAgain(t *testing.T) {
	imp := newImporter()
	o := lora.New(imp)
	add, err := commandinterpreter.NewOperation(lora.OpAdd, lora.Args{Token: "gone"})
	require.NoError(t, err)

	_, err = o.Apply(context.Background(), project, add)
	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Equal(t, "確認待ちのファイルが見つかりません。もう一度添付してください", ref.Message)

	_, err = o.Question(context.Background(), project, add)
	require.ErrorAs(t, err, &ref)
	assert.Equal(t, "確認待ちのファイルが見つかりません。もう一度添付してください", ref.Message)
	assert.Empty(t, imp.commits, "nothing is committed for a token that does not wait")
}

func TestApply_ARefusedCommitSaysWhyAndDiscardsTheFile(t *testing.T) {
	imp := newImporter()
	imp.next["style.safetensors"] = staged("tok1", "style.safetensors")
	o := lora.New(imp)
	msgs, err := o.Offer(context.Background(), project, "", []chatops.File{file("style.safetensors", "x")})
	require.NoError(t, err)
	_, yes, _ := cardOf(t, msgs[0])
	imp.commitErr = chatops.Refuse("推奨の強さは 0 より大きく 1 以下の数にしてください")

	_, err = o.Apply(context.Background(), project, yes)

	var ref *chatops.Refusal
	require.ErrorAs(t, err, &ref)
	assert.Contains(t, ref.Message, "推奨の強さは 0 より大きく 1 以下の数にしてください")
	assert.Contains(t, ref.Message, "「モデルの管理」の画面")
	assert.Equal(t, []string{"tok1"}, imp.discards)
}

func TestApply_AFaultOfTheCommitIsReturnedAndTheFileKeptForAnotherTry(t *testing.T) {
	imp := newImporter()
	imp.next["style.safetensors"] = staged("tok1", "style.safetensors")
	o := lora.New(imp)
	msgs, err := o.Offer(context.Background(), project, "", []chatops.File{file("style.safetensors", "x")})
	require.NoError(t, err)
	_, yes, _ := cardOf(t, msgs[0])
	fault := errors.New("state file is not writable")
	imp.commitErr = fault

	_, err = o.Apply(context.Background(), project, yes)

	require.ErrorIs(t, err, fault)
	assert.Empty(t, imp.discards)
	assert.Contains(t, imp.waiting, "tok1")
}

func TestQuestion_BuildsTheCardOfAFileThatWaits(t *testing.T) {
	imp := newImporter()
	imp.waiting["tok1"] = staged("tok1", "style.safetensors")
	add, err := commandinterpreter.NewOperation(lora.OpAdd, lora.Args{Token: "tok1"})
	require.NoError(t, err)

	card, err := lora.New(imp).Question(context.Background(), project, add)

	require.NoError(t, err)
	assert.Contains(t, card.Text, "全プロジェクトに効く")
	require.NotNil(t, card.Decline)
	assert.Equal(t, lora.OpDiscard, card.Decline.Type)
}

// The operations exist only as the actions of the card's buttons: the model is not told about them and never returns them.
func TestSpec_TheModelIsNotAskedForTheOperations(t *testing.T) {
	for _, op := range []commandinterpreter.OpType{lora.OpAdd, lora.OpDiscard} {
		spec, ok := commandinterpreter.SpecOf(op)
		require.True(t, ok, op)
		assert.Empty(t, spec.Example)
		assert.Empty(t, spec.Doc)
		assert.True(t, spec.Global)
	}
	assert.True(t, commandinterpreter.Operation{Type: lora.OpAdd}.Confirms())

	raw, err := commandinterpreter.NewOperation(lora.OpAdd, lora.Args{Token: "tok1"})
	require.NoError(t, err)
	_, ok := commandinterpreter.Normalize(raw)
	assert.True(t, ok, "a card's action is read back")
	_, ok = commandinterpreter.Normalize(commandinterpreter.Operation{Type: lora.OpAdd})
	assert.False(t, ok, "without the file it names, the operation is none")
}
