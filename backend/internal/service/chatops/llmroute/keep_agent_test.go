package llmroute

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// An Agent that a clause leaves as it is ("Producer は据え置きで") is never the Agent to change (#1144). With no other Agent named, the Agent is left empty, so the chat asks which.
func TestDetect_TheAgentThatIsKeptIsNotTheOneToChange(t *testing.T) {
	for message, want := range map[string]Args{
		// one Agent named, and it is the kept one: no Agent is the target
		"Producer は据え置きで、LLM を GPT にして":    {Agent: "", Role: "primary", Provider: "openai", Model: "GPT"},
		"Producer は触らずに、LLM を GPT にして":     {Agent: "", Role: "primary", Provider: "openai", Model: "GPT"},
		"Producer はキープで、LLM を GPT にして":     {Agent: "", Role: "primary", Provider: "openai", Model: "GPT"},
		"Producer は据え置きで。LLM を GPT にして":    {Agent: "", Role: "primary", Provider: "openai", Model: "GPT"},
		"LLM を GPT にして、Producer は据え置きで":    {Agent: "", Role: "primary", Provider: "openai", Model: "GPT"},
		"Producer は据え置きで、LLM を Gemini にして": {Agent: "", Role: "primary", Provider: "google", Model: "Gemini"},
		// two Agents named, one of them kept: the other one is the target
		"Producer は据え置きで、Director の LLM を GPT にして":   {Agent: "director", Role: "primary", Provider: "openai", Model: "GPT"},
		"Producer は据え置きで、Director は LLM を GPT にして":   {Agent: "director", Role: "primary", Provider: "openai", Model: "GPT"},
		"Producer は据え置きで、Director は、LLM を GPT にして":   {Agent: "director", Role: "primary", Provider: "openai", Model: "GPT"},
		"Director の LLM を GPT にして、Producer は据え置きで":   {Agent: "director", Role: "primary", Provider: "openai", Model: "GPT"},
		"Producer は据え置きで。Director の LLM を GPT にして":   {Agent: "director", Role: "primary", Provider: "openai", Model: "GPT"},
		"Director の LLM を GPT にして。Producer は据え置きにして": {Agent: "director", Role: "primary", Provider: "openai", Model: "GPT"},
		// a clause that keeps one role names the Agent whose other role is changed (unchanged: #1141)
		"Director の代替は残して、LLM を Claude Opus にして": {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
	} {
		ops := detect(message)
		require.Len(t, ops, 1, message)
		var a Args
		require.NoError(t, ops[0].DecodeArgs(&a))
		assert.Equal(t, want, a, message)
	}
}

// With the Agent left empty, the card is not made: the chat asks which Agent (the one that is kept is not changed).
func TestQuestion_NoAgentToChangeAsksWhichAndKeepsTheRouteSaved(t *testing.T) {
	f := newFake()
	ops := detect("Producer は据え置きで、LLM を GPT にして")
	require.Len(t, ops, 1)
	var a Args
	require.NoError(t, ops[0].DecodeArgs(&a))

	_, err := ask(t, f, a)

	var refusal *chatops.Refusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Message, "どの Agent")
	assert.Empty(t, f.saved)
}
