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
		"Director の代替は、LLM を GPT にして":              {Agent: "director", Role: "fallback", Provider: "openai", Model: "GPT"}, // a topic clause names the role
		"Producer の代替は、LLM を Gemini にして":           {Agent: "producer", Role: "fallback", Provider: "google", Model: "Gemini"},
		"Director のバックアップは、LLM を GPT にして":          {Agent: "director", Role: "fallback", Provider: "openai", Model: "GPT"},
		"Producer のフォールバックは、LLM を Gemini にして":      {Agent: "producer", Role: "fallback", Provider: "google", Model: "Gemini"},
		"Producer の fallback は、LLM を Gemini にして":   {Agent: "producer", Role: "fallback", Provider: "google", Model: "Gemini"},
		"Producer の代替は、LLM を Claude Opus に切り替えて":   {Agent: "producer", Role: "fallback", Provider: "anthropic", Model: "Claude Opus"},
		"Director の代替は残して、LLM を Claude Opus にして":   {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"}, // a clause that keeps it names no role to change
		"Director の代替は変えずに、LLM を Claude Opus にして":  {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director の代替はそのままで、LLM を Claude Opus にして": {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director の代替は触らずに、LLM を Claude Opus にして":  {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		// the Agent is the one the clause before the request names ("Director は、"), though another Agent is named after or before it
		"Producer のバックアップはそのまま。Director は、LLM を Claude Opus にして": {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Director は、LLM を Claude Opus にして。Producer のバックアップはそのまま": {Agent: "director", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		// the Agent of a clause that keeps a route is not the one asked: two Agents are named, so the chat asks which (never Producer)
		"Producer は据え置きで、LLM を Claude Opus にして、Director に": {Agent: "", Role: "primary", Provider: "anthropic", Model: "Claude Opus"},
		"Producer は触らずに、LLM を Gemini にして。Director で":       {Agent: "", Role: "primary", Provider: "google", Model: "Gemini"},
		"Producer の代替は、Director の LLM を GPT にして":           {Agent: "director", Role: "fallback", Provider: "openai", Model: "GPT"},
		"Art Director と Director は、LLM を Gemini にして":       {Agent: "", Role: "primary", Provider: "google", Model: "Gemini"}, // two Agents: the chat asks which
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
		"Director の LLM を Claude Opus にして、Producer はキープにして", // no model's name
		"Director の LLM を Claude Opus にして、Producer はデフォルトのままにして",
		"Director の LLM を Claude Opus にして、Producer は JSON で返すようにして",
		"Director の LLM を Claude Opus にして、Producer は 3 回までにして",
		"Director の LLM を Claude Opus にして、Producer は Director と同じにして",
		"Director の LLM を Claude Opus にして、Producer は 3 回にして", // a count, a duration, an Agent's name or a degree is no model's
		"Director の LLM を Claude Opus にして、Producer は 5 秒にして",
		"Director の LLM を Claude Opus にして、Producer は Director と一緒にして",
		"Director の LLM を Claude Opus にして、Producer は Director の設定にして",
		"Director の LLM を Claude Opus にして、Producer は丁寧な English にして",
		"Director の LLM を Claude Opus にして、Producer は Director より速いのにして",
		"Director の LLM を Claude Opus にして、Producer はそのままにして",
	} {
		t.Run(message, func(t *testing.T) {
			ops := detect(message)
			if assert.Len(t, ops, 1) {
				var a Args
				require.NoError(t, ops[0].DecodeArgs(&a))
				assert.Equal(t, "Claude Opus", a.Model)
			}
		})
	}
	for _, message := range []string{ // an ASCII letter or digit, or a katakana, makes a name a model's; a degree word does not take that away when a model token stands in the name
		"Director の LLM を Claude Opus にして、Producer は gpt-4o にして",
		"Director の LLM を Claude Opus にして、Producer は o3 mini にして",
		"Director の LLM を Claude Opus にして、Producer は Llama 3 にして",
		"Director の LLM を Claude Opus にして、Producer は Mistral Large にして",
		"Director の LLM を Claude Opus にして、Producer は ミストラル にして",
		"Director の LLM を Claude Opus にして、Producer は Qwen 3 の 32B にして", // an ASCII word beside Japanese is a model's name
		"Director の LLM を Claude Opus にして、Producer は Llama 3 の 70B にして",
		"Director の LLM を Claude Opus にして、Producer は DeepSeek 最新にして",
		"Director の LLM を Claude Opus にして、Producer は軽い Gemma にして",
		"Director の LLM を Claude Opus にして、Producer は速い Gemma にして",
		"Director の LLM を Claude Opus にして、Producer は遅い Mistral にして",
		"Director の LLM を Claude Opus にして、Producer は Mixtral 8x7B にして",
		"Director の LLM を Claude Opus にして、Producer は R1 の 高速版にして",
		"Director の LLM を Claude Opus にして、Producer は o1 の 長考版にして",
		"Director の LLM を Claude Opus にして、Producer は o3 の 速いやつにして",
		"Director の LLM を Claude Opus にして、Producer は o4 の 速い方にして",
		"Director の LLM を Claude Opus にして、Producer は ミストラル 長文版にして",
		"Director の LLM を Claude Opus にして、Producer は ミストラル の長文版にして",
		"Director の LLM を Claude Opus にして、Producer は ラマ 3 の 速い版にして",
		"Director の LLM を Claude Opus にして、Producer は Phi の 速い版にして", // an ASCII token of 3 letters
		"Director の LLM を Claude Opus にして、Producer は ラー の 速い版にして",  // a katakana run that is 2 characters only with its 「ー」
		"Director の LLM を Claude Opus にして。Storyboard は R1 の 速いやつにして",
		"Director の LLM を Claude Opus にして、Producer の代替は o3 mini にして",
	} {
		t.Run(message, func(t *testing.T) { assert.Empty(t, detect(message)) })
	}
}

// The trade-off for the second route: a name with a katakana word of 2 or more characters may be a model's even beside a count or a degree ("ミストラル 長文版" cannot be told from "リトライ 3 回" without a
// list), so a single request beside such a clause is left to the model (which asks or answers).
func TestDetect_KatakanaWithADegreeWordIsLeftToTheModel(t *testing.T) {
	for _, message := range []string{
		"Director の LLM を Claude Opus にして、Producer は リトライ 3 回にして",
		"Director の LLM を Claude Opus にして、Producer は タイムアウト 5 秒にして",
		"Director の LLM を Claude Opus にして、Producer は ステップ 3 つにして",
		"Director の LLM を Claude Opus にして、Producer は ミストラル 3 回にして",
		"Director の LLM を Claude Opus にして、Producer は ミストラル 長文版にして",
		"Director の LLM を Claude Opus にして、Producer は コード 速い版にして",
	} {
		t.Run(message, func(t *testing.T) { assert.Empty(t, detect(message)) })
	}
}
