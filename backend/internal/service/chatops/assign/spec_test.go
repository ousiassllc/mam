package assign

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

func TestSpecIsGlobalAndAsksFirstInEveryMode(t *testing.T) {
	spec, ok := commandinterpreter.SpecOf(OpSetModelAssignment)

	require.True(t, ok)
	assert.True(t, spec.Global)
	assert.True(t, spec.Confirm)
	assert.True(t, spec.AnyMode)
	assert.False(t, spec.Immediate)
	assert.Contains(t, spec.Doc, `"set_model_assignment"`)
	assert.True(t, len(spec.Doc) > 0 && spec.Doc[len(spec.Doc)-1] == '\n')
}

// The model's word alone does not change the assignment: the message has to ask for it.
func TestAskedNeedsAModelAndAChange(t *testing.T) {
	for _, message := range []string{
		"生成の画像モデルの 新規参照 を flux にして",
		"Quality MAX の動画モデルを kling-4.0 に",
		"1080p で 3 Take",
		"BALANCED の代替を seedance-2.0 に変えて",
		"プレビューのモデルを Nano Banana にしたい",
		"動画モデルの割り当てを変更して",
	} {
		assert.True(t, Asked(message), message)
	}
	for _, message := range []string{
		"カメラを固定して",
		"予算を $30 に変えて",
		"OpenAI の API キーを設定したい",
		"今の画像モデルは何ですか？",
		"動画モデルを教えて",
		"Producer の LLM を Claude にして",
		"Kling は OpenRouter 経由で使いたい",
		"Kling の API キーを設定したい",
		"",
	} {
		assert.False(t, Asked(message), message)
	}
}

func TestCleanNormalisesTheArguments(t *testing.T) {
	op := opOf(t, Args{Target: "Quality MAX", Role: "代替", Model: " kling-4.0 ", Resolution: " 1080p ", Takes: -2})

	got, ok := clean(op)

	require.True(t, ok)
	var a Args
	require.NoError(t, got.DecodeArgs(&a))
	assert.Equal(t, Args{Target: "max_quality", Role: "fallback", Model: "kling-4.0", Resolution: "1080p"}, a)

	got, ok = clean(opOf(t, Args{Target: "新規参照", Model: "flux"}))
	require.True(t, ok)
	require.NoError(t, got.DecodeArgs(&a))
	assert.Equal(t, "new_reference", a.Target)
	assert.Equal(t, "primary", a.Role)

	got, ok = clean(opOf(t, Args{Target: "どれか"}))
	require.True(t, ok)
	var none Args
	require.NoError(t, got.DecodeArgs(&none))
	assert.Empty(t, none.Target, "a target that is none is left for the chat to ask about, not guessed")
}

func TestCleanDropsArgumentsThatAreNotReadable(t *testing.T) {
	_, ok := clean(commandinterpreter.Operation{Type: OpSetModelAssignment, Args: []byte(`{"takes":"three"}`)})

	assert.False(t, ok)
}
