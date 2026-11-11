package llmroute

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsked_ARequestInTheFormOfAQuestionIsARequest(t *testing.T) {
	for _, message := range []string{
		"Director の LLM を Claude Opus にしてくれる？",
		"Director の LLM を Claude Opus にしてもらえますか？",
		"Director の LLM を Claude Opus にしてくれますか",
		"Director の LLM を Claude Opus に変えてください",
	} {
		assert.True(t, Asked(message), message)
	}
	for _, message := range []string{
		"Director の LLM は Claude Opus にしてある？",
		"Director の LLM は何にしてますか？",
		"Director の LLM 設定を教えてくれる？",
	} {
		assert.False(t, Asked(message), message)
	}
}

func TestDetect_AMessageThatChangesTwoRoutesIsNotReadAsOne(t *testing.T) {
	assert.Empty(t, detect("Director の LLM を Claude Opus に変更して。Producer の代替を GPT にして"))
}

func TestDetect_TheSentenceThatAsksIsTheOneThatIsRead(t *testing.T) {
	ops := detect("Director の LLM を確認して。Producer の代替を GPT にして")

	require.Len(t, ops, 1)
	var a Args
	require.NoError(t, ops[0].DecodeArgs(&a))
	assert.Equal(t, Args{Agent: "producer", Role: "fallback", Provider: "openai", Model: "GPT"}, a)
}
