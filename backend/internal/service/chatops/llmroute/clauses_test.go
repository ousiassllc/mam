package llmroute

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
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

// The clauses end where outofscope's end (nlu.ClauseEnd): ！ ？ ! ? and an ASCII period split a message as 。 does, so one clause's Agent, role and model are not mixed with another's.
func TestDetect_AMessageThatChangesTwoRoutesIsNotReadAsOneWhateverEndsTheClause(t *testing.T) {
	for _, message := range []string{
		"Director の LLM を Claude Opus にして！ Producer の代替を GPT にして",
		"Director の LLM を Claude Opus にして! Producer の代替を GPT にして",
		"Director の LLM を Claude Opus にして. Producer の代替を GPT にして",
		"Director の LLM を Claude Opus にして、Producer の代替を GPT にして",
		"Director の LLM を Claude Opus にして、Producer は GPT にして", // the second sentence has no LLM word, only an Agent and a model
		"Director の LLM を Claude Opus にして、代替は GPT にして",
	} {
		assert.Empty(t, detect(message), message)
	}
}

// The model is the name after the last は/を particle before 「にして」: 「Director は LLM を Claude Opus にして」 names Claude Opus.
func TestDetect_TheModelIsTheNameAfterTheLastParticle(t *testing.T) {
	for message, model := range map[string]string{
		"Director は LLM を Claude Opus にして":  "Claude Opus",
		"Producer は代替を GPT にして":             "GPT",
		"Director の LLM を、Claude Opus にして":  "Claude Opus", // no comma in the name
		"Director の LLM を， Claude Opus にして": "Claude Opus",
	} {
		ops := detect(message)
		require.Len(t, ops, 1, message)
		var a Args
		require.NoError(t, ops[0].DecodeArgs(&a))
		assert.Equal(t, model, a.Model, message)
	}
}

// A model name that fits as many models as a card holds as buttons (chatops.MaxChoices) is asked as buttons; one more is asked to be narrowed down, with no button.
func TestFallback_TheButtonsOfAModelNameStopAtTheCardLimit(t *testing.T) {
	list := func(n int) llmmodels.List {
		l := llmmodels.List{Provider: "openai", Source: llmmodels.SourceAPI}
		for i := range n {
			l.Models = append(l.Models, llmmodels.Model{ID: fmt.Sprintf("mega-%d", i)})
		}
		return l
	}
	f := newFake()
	f.lists["openai"] = list(chatops.MaxChoices)
	card, err := ask(t, f, Args{Agent: "producer", Provider: "openai", Model: "mega"})
	require.NoError(t, err)
	assert.Len(t, card.Choices, chatops.MaxChoices)

	f.lists["openai"] = list(chatops.MaxChoices + 1)
	card, err = ask(t, f, Args{Agent: "producer", Provider: "openai", Model: "mega"})
	assert.Contains(t, refusal(t, err), "多すぎます")
	assert.Empty(t, card.Choices)
}

// The Agent may stand in a clause of its own ("Director は、LLM を Claude Opus にして"): the one clause that asks has the model, the message has the Agent and the role.
func TestDetect_TheAgentInAnotherClauseIsTheAgentOfTheOneRequest(t *testing.T) {
	for message, want := range map[string]Args{
		"Director は、LLM を Claude Opus にして":         {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director、LLM を Claude Opus にして":           {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director に、LLM を Claude Opus にして":         {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"まず Director は、LLM を Claude Opus に切り替えて":   {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Producer は、代替を GPT にして":                   {Agent: "producer", Role: "fallback", Provider: "openai", Model: "GPT"},
		"LLM を Claude Opus にして、Director で":         {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director の代替は、LLM を GPT にして":              {Agent: "director", Role: "primary", Provider: "openai", Model: "GPT"}, // the role is the request clause's own
		"Director の代替は残して、LLM を Claude Opus にして":   {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director の代替はそのままで、LLM を Claude Opus にして": {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director の代替は触らずに、LLM を Claude Opus にして":  {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		// two Agents are named and the sentence that asks names none: the Agent is left empty (the chat asks which), the role is still the sentence's
		"Producer のバックアップはそのまま。Director は、LLM を Claude Opus にして": {Agent: "", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
	} {
		ops := detect(message)
		require.Len(t, ops, 1, message)
		var a Args
		require.NoError(t, ops[0].DecodeArgs(&a))
		assert.Equal(t, want, a, message)
	}
}

// A word that is no model name is no second route: it is not counted as a clause that changes a model.
func TestDetect_AWordThatIsNoModelNameIsNotASecondRoute(t *testing.T) {
	for _, message := range []string{
		"Director の LLM を Claude Opus にして、Producer は据え置きにして",
		"Director の LLM を Claude Opus にして。モデルはそのままにして",
		"Director の LLM を Claude Opus にして。Director は慎重な性格にして",
	} {
		ops := detect(message)
		require.Len(t, ops, 1, message)
		var a Args
		require.NoError(t, ops[0].DecodeArgs(&a))
		assert.Equal(t, "Claude Opus", a.Model, message)
	}
	for _, message := range []string{ // an ASCII letter or digit, or a katakana, makes a name a model's
		"Director の LLM を Claude Opus にして、Producer は gpt-4o にして",
		"Director の LLM を Claude Opus にして、Producer は o3 mini にして",
		"Director の LLM を Claude Opus にして、Producer は Llama 3 にして",
		"Director の LLM を Claude Opus にして、Producer は Mistral Large にして",
		"Director の LLM を Claude Opus にして、Producer は ミストラル にして",
		"Director の LLM を Claude Opus にして、Producer の代替は o3 mini にして",
	} {
		assert.Empty(t, detect(message), message)
	}
}
