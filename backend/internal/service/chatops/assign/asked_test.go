package assign

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A number of takes alone is not the setting of a Quality Policy: "3 テイクで作り直して" asks to make the shot again.
func TestAsked_ANumberOfTakesAloneIsNotTheSetting(t *testing.T) {
	for _, message := range []string{
		"3 テイクで作り直して",
		"このショットを 3 take で撮り直して",
		"3 テイクで作って",
	} {
		assert.False(t, Asked(message), message)
	}
	for _, message := range []string{
		"1080p で 3 Take",
		"Quality MAX を 3 テイクにして",
		"3 take にして",
	} {
		assert.True(t, Asked(message), message)
	}
}

// "代替" and "モデル" beside an Agent are the LLM of its route (package llmroute), not this assignment.
func TestAsked_AnAgentsFallbackModelIsTheLLMNotThisAssignment(t *testing.T) {
	for _, message := range []string{
		"Producer の代替モデルを Claude にして",
		"Director のプライマリのモデルを gpt-5 に変えて",
	} {
		assert.False(t, Asked(message), message)
	}
	assert.True(t, Asked("BALANCED の代替モデルを seedance-2.0 に変えて"))
	assert.True(t, Asked("Producer の動画モデルを kling-4.0 に"), "a medium named: the target is not an Agent's LLM")
}

func TestAsked_ARequestInTheFormOfAQuestionIsARequest(t *testing.T) {
	assert.True(t, Asked("画像モデルを Nano Banana にしてくれる？"))
	assert.False(t, Asked("今の画像モデルは何？"))
}
