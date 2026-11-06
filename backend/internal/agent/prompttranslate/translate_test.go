package prompttranslate_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
)

// answer is a Completer that returns canned text and records the call.
type answer struct {
	text  string
	err   error
	agent llm.Agent
	req   llm.Request
	calls int
}

func (a *answer) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	a.calls++
	a.agent, a.req = agent, req
	return llm.Response{Text: a.text}, a.err
}

var target = llm.Target{Provider: llm.ProviderOpenAI, Model: "m"}

func TestNeeds(t *testing.T) {
	for text, want := range map[string]bool{
		"a fox": false, "café": false, "😀 4k 16:9": false, "": false, "夕暮れ": true, "fox 狐": true, "Привет": true, "한글": true,
		// Common-script letters (µ U+00B5, ʼ U+02BC) are not "another script": plain English stays English.
		"5µm particles": false, "ʼtis a fox": false, "µ": false, "ʼ": false,
		"Ελληνικά": true, "ひらがな": true, "カタカナ": true, "日本語 and English": true, "5µm 粒子": true,
		"über señor café": false, "ＡＢＣ": false, "ＡＢＣ 狐": true,
		" \t\n": false, "1234 !?#%": false, "😀🎉": false,
	} {
		assert.Equal(t, want, prompttranslate.Needs(text), text)
	}
}

// #1072: a letter of the Common script is no other script, so a text of nothing else is not asked. The prolonged sound mark ー
// (U+30FC, a letter of the category Lm) is one; kana, kanji and 々 (Han) are not.
func TestNeedsAtTheBoundaryOfTheCommonScript(t *testing.T) {
	for text, want := range map[string]bool{
		"ー": false, "ーーー": false, "ー ー": false, "ー 4k": false, "〆": false,
		"コーヒー": true, "ー狐": true, "ー fox 狐": true, "ァ": true, "々": true, "ー Ω": true,
	} {
		assert.Equal(t, want, prompttranslate.Needs(text), text)
		assert.Equal(t, want, len(prompttranslate.Select([]string{text})) == 1, text+" (Select)")
	}
}

func TestSelectKeepsTheDistinctNonEnglishTextsInOrder(t *testing.T) {
	assert.Equal(t, []string{"狐", "猫"}, prompttranslate.Select([]string{"a fox", "狐", "", "猫", "狐", "dog"}))
	assert.Empty(t, prompttranslate.Select([]string{"a fox", ""}))
}

func TestTranslateAsksThePromptPlannersRouteOnceAndReturnsTheAnswerInOrder(t *testing.T) {
	a := &answer{text: "```json\n{\"texts\": [\" a fox \", \"a cat\"]}\n```"}
	tr := prompttranslate.New(a, target)
	got, err := tr.Translate(t.Context(), []string{"狐", "猫"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a fox", "a cat"}, got)
	assert.Equal(t, 1, a.calls)
	assert.Equal(t, llm.AgentPromptPlanner, a.agent)
	assert.Equal(t, prompttranslate.Agent, a.agent)
	assert.True(t, a.req.JSON)
	var sent struct{ Texts []string }
	require.NoError(t, json.Unmarshal([]byte(a.req.User), &sent))
	assert.Equal(t, []string{"狐", "猫"}, sent.Texts, "the texts are data in a JSON object, not part of the instructions")
	assert.Equal(t, target, tr.Target())
}

func TestTranslateCallsNothingForNoText(t *testing.T) {
	a := &answer{err: errors.New("must not be called")}
	got, err := prompttranslate.New(a, target).Translate(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Zero(t, a.calls)
}

func TestTranslateRefusesAnAnswerThatIsNotATextForEveryText(t *testing.T) {
	for name, text := range map[string]string{
		"too few":  `{"texts": ["a fox"]}`,
		"too many": `{"texts": ["a fox", "a cat", "a dog"]}`,
		"empty":    `{"texts": ["a fox", " "]}`,
	} {
		_, err := prompttranslate.New(&answer{text: text}, target).Translate(t.Context(), []string{"狐", "猫"})
		assert.ErrorIs(t, err, prompttranslate.ErrNotTranslated, name)
	}
	_, err := prompttranslate.New(&answer{text: "no json at all"}, target).Translate(t.Context(), []string{"狐"})
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
}

// A text with a Greek letter but nothing to translate (50Ω resistor) comes back as it was: that is no failure, the text stays
// as it was typed and the others are still translated.
func TestTranslateKeepsATextTheLLMReturnedUnchanged(t *testing.T) {
	a := &answer{text: `{"texts": ["50Ω resistor", "a fox", " π day "]}`}
	got, err := prompttranslate.New(a, target).Translate(t.Context(), []string{"50Ω resistor", "狐", " π day "})
	require.NoError(t, err)
	assert.Equal(t, []string{"50Ω resistor", "a fox", " π day "}, got)
}

func TestTranslateReturnsTheFailureOfTheLLM(t *testing.T) {
	_, err := prompttranslate.New(&answer{err: llm.ErrNoAPIKey}, target).Translate(t.Context(), []string{"狐"})
	assert.ErrorIs(t, err, llm.ErrNoAPIKey)
}

// #972: the instruction tells the LLM to keep the weight syntax, with the example of a weighted word, and what is sent is the text as it
// was typed (the brackets and the numbers are not touched on the way in).
func TestTheInstructionKeepsTheWeightSyntax(t *testing.T) {
	a := &answer{text: `{"texts": ["(red fox:1.3), sunset"]}`}
	got, err := prompttranslate.New(a, target).Translate(t.Context(), []string{"(赤い狐:1.3)、夕暮れ"})
	require.NoError(t, err)
	assert.Equal(t, []string{"(red fox:1.3), sunset"}, got)
	for _, want := range []string{"(token:weight)", "(word)", "[word]", "keep every parenthesis, bracket, colon and number exactly as written", "(red fox:1.3)"} {
		assert.Contains(t, a.req.System, want)
	}
	var sent struct{ Texts []string }
	require.NoError(t, json.Unmarshal([]byte(a.req.User), &sent))
	assert.Equal(t, []string{"(赤い狐:1.3)、夕暮れ"}, sent.Texts)
}

// An English prompt with weights is never sent to the LLM: it reaches the model as it was typed.
func TestEnglishPromptsWithWeightsAreNotTranslated(t *testing.T) {
	for _, text := range []string{"(red fox:1.3), [blurry], ((sunset))", "a (fox:0.8) in the snow", `\(not a weight\)`} {
		assert.False(t, prompttranslate.Needs(text), text)
		assert.Empty(t, prompttranslate.Select([]string{text}), text)
	}
}

func TestHasWeights(t *testing.T) {
	for text, want := range map[string]bool{
		"(fox:1.3)": true, "a (fox) here": true, "((fox))": true, "(fox : 1.3)": true, "[fox]": false, // ComfyUI does not read [ ]
		"a fox": false, "": false, `\(fox\)`: false, `\[fox\]`: false, "(unclosed": false, "closed)": false, "fox:1.3": false,
		"(赤い狐:1.3)": true,
	} {
		assert.Equal(t, want, prompttranslate.HasWeights(text), text)
	}
}

// What the translation lost of the weights is found by comparing the numbers and the brackets, not by trusting the LLM.
func TestLostWeights(t *testing.T) {
	for _, c := range []struct {
		name, source, english string
		lost                  []string
	}{
		{"kept", "(赤い狐:1.3)、夕暮れ", "(red fox:1.3), sunset", nil},
		{"the spelling of a number is not its value", "(狐:1.30)", "(fox:1.3)", nil},
		{"more is fine", "狐", "(fox:1.2)", nil},
		{"the number is gone", "(狐:1.3)", "(fox)", []string{"重み :1.3"}},
		{"the brackets are gone", "(狐:1.3)", "fox:1.3", []string{"重み :1.3", "丸括弧 ( )"}},
		{"the number changed", "(狐:1.3)", "(fox:1.5)", []string{"重み :1.3"}},
		{"one of two numbers", "(狐:1.3)、(雪:0.8)", "(fox:1.3), snow", []string{"重み :0.8", "丸括弧 ( )"}},
		{"the same number twice, once kept", "(狐:1.2)、(猫:1.2)", "(fox:1.2), cat", []string{"重み :1.2", "丸括弧 ( )"}},
		{"plain brackets", "((狐))、[雪]", "fox, snow", []string{"丸括弧 ( )", "角括弧 [ ]"}},
		{"plain brackets kept", "((狐))、[雪]", "((fox)), [snow]", nil},
		{"an escaped bracket is text", `\(狐\)`, "fox", nil},
		{"nothing to keep", "狐", "fox", nil},
	} {
		assert.Equal(t, c.lost, prompttranslate.LostWeights(c.source, c.english), c.name)
	}
}
