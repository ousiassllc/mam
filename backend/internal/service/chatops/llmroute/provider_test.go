package llmroute

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// A provider added to llm.Providers must be nameable in a chat message and listed for the model.
func TestProviderNamesCoverEveryProvider(t *testing.T) {
	spec, ok := commandinterpreter.SpecOf(OpSetLLMAssignment)
	require.True(t, ok)
	var ids []string
	for _, p := range providerNames {
		ids = append(ids, p.id)
	}
	for _, p := range llm.Providers() {
		assert.Contains(t, ids, string(p), "providerNames has no entry for the provider")
		got, found := providerOf(string(p))
		assert.True(t, found, string(p))
		assert.Equal(t, string(p), got, "the ID of a provider names it")
		assert.Contains(t, spec.Doc, `"`+string(p)+`"`, "the Doc lists every provider")
	}
	assert.Len(t, ids, len(llm.Providers()), "providerNames has a provider llm.Providers does not")
}

func TestProviderOf_AnOpenWeightGPTModelIsLocalNotOpenAI(t *testing.T) {
	for text, want := range map[string]string{
		"gpt-oss":       "local",
		"gpt-oss:20b":   "local",
		"GPT":           "openai",
		"gpt-5":         "openai",
		"gpt-5-codex":   "chatgpt-codex",
		"Claude Opus":   "anthropic",
		"gemini-2.5":    "google",
		"ChatGPT 経由で使う": "chatgpt-subscription",
	} {
		got, ok := providerOf(text)
		require.True(t, ok, text)
		assert.Equal(t, want, got, text)
	}
}
