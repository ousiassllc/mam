package charref_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/charref"
)

func detect(t *testing.T, message string) []charref.Args {
	t.Helper()
	var out []charref.Args
	for _, op := range commandinterpreter.Detected(message) {
		if op.Type != charref.OpAssign {
			continue
		}
		var a charref.Args
		require.NoError(t, op.DecodeArgs(&a))
		out = append(out, a)
	}
	return out
}

func TestDetect_ReadsTheCharacterAndTheSlotWithoutAModel(t *testing.T) {
	for message, want := range map[string]charref.Args{
		"この画像を主人公の顔にして":         {Character: "主人公", Slot: "face"},
		"この画像をミオの顔にして":          {Character: "ミオ", Slot: "face"},
		"添付の写真をミオの全身の参照にして":     {Character: "ミオ", Slot: "body"},
		"ミオの顔の参照にこの画像を使って":      {Character: "ミオ", Slot: "face"},
		"この画像を顔の参照にして":          {Slot: "face"},
		"この写真を体の参照に設定して":        {Slot: "body"},
		"この画像をヒロインのフェイスにして":     {Character: "ヒロイン", Slot: "face"},
		"この画像を Mio の face にして":  {Character: "Mio", Slot: "face"},
		"この画像を Mio の顔の参照に割り当てて": {Character: "Mio", Slot: "face"},
		"この画像をミオの体にして":          {Character: "ミオ", Slot: "body"},
		"この画像を体の参照にして":          {Slot: "body"},
		"この画像を主人公の身体の参照にして":     {Character: "主人公", Slot: "body"},
		"この画像をミオの体型の参照にして":      {Character: "ミオ", Slot: "body"},
		"この画像を全体の顔にして":          {Character: "全体", Slot: "face"},
	} {
		got := detect(t, message)
		require.Len(t, got, 1, message)
		assert.Equal(t, want, got[0], message)
	}
}

func TestDetect_DoesNotMistakeADirectionAboutAFaceForAReference(t *testing.T) {
	for _, message := range []string{
		"主人公の顔を笑顔にして",
		"この写真の顔を明るくして",
		"顔を大きく映して",
		"主人公の顔の参照が欲しい", // asks for nothing to be done with an image
		"この画像の雰囲気にして",
		"カメラを固定して",
		"画像を添付しました",
		"ミオの表情を変えて",
		// words with 体 in them are no body slot
		"全体の雰囲気はこの参照画像にして",
		"この参照画像を全体に使って",
		"この写真を具体的な参考にして",
		"この写真を体験版の参考にして",
		"この画像を主体にして",
		"この画像をシーン全体の参照にして",
	} {
		assert.Empty(t, detect(t, message), message)
		assert.False(t, charref.Asks(message), message)
	}
}

func TestAsked_TheModelsWordAloneIsNotEnough(t *testing.T) {
	spec, ok := commandinterpreter.SpecOf(charref.OpAssign)
	require.True(t, ok)
	assert.True(t, spec.Confirm && spec.AnyMode)
	assert.False(t, spec.Global, "it changes this Project's Character only")
	assert.False(t, spec.Immediate)
	op, _ := commandinterpreter.NewOperation(charref.OpAssign, charref.Args{Character: "ミオ", Slot: "face"})

	assert.Empty(t, commandinterpreter.Unasked([]commandinterpreter.Operation{op}, "カメラを固定して"))
	assert.Empty(t, commandinterpreter.Unasked([]commandinterpreter.Operation{op}, "ミオの顔を笑顔にして"))
	assert.Len(t, commandinterpreter.Unasked([]commandinterpreter.Operation{op}, "この画像をミオの顔にして"), 1)
}

func TestClean_KeepsTheNameAndDropsASlotTheModelInvented(t *testing.T) {
	op, _ := commandinterpreter.NewOperation(charref.OpAssign, charref.Args{Character: " ミオ ", Slot: "顔"})
	cleaned, ok := commandinterpreter.Normalize(op)
	require.True(t, ok)
	var a charref.Args
	require.NoError(t, cleaned.DecodeArgs(&a))
	assert.Equal(t, charref.Args{Character: "ミオ", Slot: "face"}, a)

	op, _ = commandinterpreter.NewOperation(charref.OpAssign, charref.Args{Character: "ミオ", Slot: "hands"})
	cleaned, _ = commandinterpreter.Normalize(op)
	var invented charref.Args
	require.NoError(t, cleaned.DecodeArgs(&invented))
	assert.Empty(t, invented.Slot)
}
