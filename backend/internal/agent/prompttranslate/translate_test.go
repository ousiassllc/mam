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
	} {
		assert.Equal(t, want, prompttranslate.Needs(text), text)
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

func TestTranslateRefusesAnAnswerThatIsNotATranslationOfEveryText(t *testing.T) {
	for name, text := range map[string]string{
		"too few":   `{"texts": ["a fox"]}`,
		"too many":  `{"texts": ["a fox", "a cat", "a dog"]}`,
		"unchanged": `{"texts": ["a fox", "猫"]}`,
		"empty":     `{"texts": ["a fox", " "]}`,
	} {
		_, err := prompttranslate.New(&answer{text: text}, target).Translate(t.Context(), []string{"狐", "猫"})
		assert.ErrorIs(t, err, prompttranslate.ErrNotTranslated, name)
	}
	_, err := prompttranslate.New(&answer{text: "no json at all"}, target).Translate(t.Context(), []string{"狐"})
	assert.ErrorIs(t, err, llm.ErrInvalidOutput)
}

func TestTranslateReturnsTheFailureOfTheLLM(t *testing.T) {
	_, err := prompttranslate.New(&answer{err: llm.ErrNoAPIKey}, target).Translate(t.Context(), []string{"狐"})
	assert.ErrorIs(t, err, llm.ErrNoAPIKey)
}
