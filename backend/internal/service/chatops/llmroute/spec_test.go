package llmroute

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
)

func TestSpecIsGlobalAndAsksFirstInEveryMode(t *testing.T) {
	spec, ok := commandinterpreter.SpecOf(OpSetLLMAssignment)

	require.True(t, ok)
	assert.True(t, spec.Global)
	assert.True(t, spec.Confirm)
	assert.True(t, spec.AnyMode)
	assert.False(t, spec.Immediate)
	assert.Contains(t, spec.Doc, `"set_llm_assignment"`)
	assert.Contains(t, spec.Doc, `"art_director"`, "the Agents the assignment has are listed for the model")
	assert.Equal(t, byte('\n'), spec.Doc[len(spec.Doc)-1])
}

// The model's word alone does not change the assignment: the message has to ask for it, and about an Agent's LLM (not the image or video models).
func TestAskedNeedsAnAgentItsLLMAndAChange(t *testing.T) {
	for _, message := range []string{
		"Director の LLM を Claude Opus にして",
		"Producer の代替を GPT にして",
		"Art Director のモデルを Gemini に変えて",
		"prompt_planner のプライマリを gpt-5 にしたい",
	} {
		assert.True(t, Asked(message), message)
	}
	for _, message := range []string{
		"カメラを固定して",
		"Director の LLM は何ですか？",
		"Quality MAX の動画モデルを kling-4.0 に",
		"生成の画像モデルの 新規参照 を flux にして",
		"LLM を変えたい",
		"OpenAI の API キーを設定したい",
		"動画は縦型の15秒にして",
		"出演者はプロのモデルを入れて",
		"Producer みたいなモデルさんを使って",
		"",
	} {
		assert.False(t, Asked(message), message)
	}
}

func TestDetectReadsTheAgentTheRoleAndTheModelWithoutTheModel(t *testing.T) {
	ops := detect("Producer の代替を GPT にして")

	require.Len(t, ops, 1)
	var a Args
	require.NoError(t, ops[0].DecodeArgs(&a))
	assert.Equal(t, Args{Agent: "producer", Role: "fallback", Provider: "openai", Model: "GPT"}, a)

	ops = detect("Art Director の LLM を Claude Opus にして")
	require.Len(t, ops, 1)
	var b Args
	require.NoError(t, ops[0].DecodeArgs(&b))
	assert.Equal(t, Args{Agent: "art_director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"}, b, "Art Director, not Director")

	assert.Empty(t, detect("Director の LLM を変えたい"), "no model named: the model's reading, or nothing")
	assert.Empty(t, detect("カメラを固定して"))
}

func TestCleanNormalisesTheArguments(t *testing.T) {
	got, ok := clean(opOf(t, Args{Agent: " director ", Role: "代替", Provider: "Claude", Model: " claude-opus "}))

	require.True(t, ok)
	var a Args
	require.NoError(t, got.DecodeArgs(&a))
	assert.Equal(t, Args{Agent: "director", Role: "fallback", Provider: "anthropic", Model: "claude-opus"}, a)

	got, ok = clean(opOf(t, Args{Agent: "director", Provider: "cohere", Model: "x"}))
	require.True(t, ok)
	var none Args
	require.NoError(t, got.DecodeArgs(&none))
	assert.Empty(t, none.Provider, "a provider that is none is left out, not guessed")
	assert.Equal(t, "primary", none.Role)

	_, ok = clean(commandinterpreter.Operation{Type: OpSetLLMAssignment, Args: []byte(`{"model":3}`)})
	assert.False(t, ok)
}

func TestProviderOfTakesTheLongestName(t *testing.T) {
	for text, want := range map[string]string{
		"Claude": "anthropic", "claude-opus-4-5": "anthropic", "GPT-5": "openai", "ChatGPT": "chatgpt-subscription", "ChatGPT Codex": "chatgpt-codex",
		"Gemini": "google", "ollama": "local", "chatgpt-subscription": "chatgpt-subscription", "openai": "openai",
	} {
		got, ok := providerOf(text)
		assert.True(t, ok, text)
		assert.Equal(t, want, got, text)
	}
	_, ok := providerOf("cohere")
	assert.False(t, ok)
}
