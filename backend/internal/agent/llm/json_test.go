package llm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// Fake is a Completer that returns canned text and records what it was asked.
type fakeCompleter struct {
	text    string
	err     error
	agent   llm.Agent
	request llm.Request
}

func (f *fakeCompleter) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.agent, f.request = agent, req
	return llm.Response{Text: f.text, Provider: llm.ProviderAnthropic, Model: "m"}, f.err
}

type answer struct {
	Title string `json:"title"`
	N     int    `json:"n"`
}

func TestCompleteJSON_DecodesPlainFencedAndChattyAnswers(t *testing.T) {
	for name, text := range map[string]string{
		"plain":  `{"title": "t", "n": 2}`,
		"fenced": "```json\n{\"title\": \"t\", \"n\": 2}\n```",
		"prose":  "はい、こちらです。\n{\"title\": \"t\", \"n\": 2}\n以上です。",
	} {
		f := &fakeCompleter{text: text}
		got, err := llm.CompleteJSON[answer](context.Background(), f, llm.AgentWriter, llm.Request{User: "x"})
		require.NoError(t, err, name)
		assert.Equal(t, answer{Title: "t", N: 2}, got, name)
		assert.Equal(t, llm.AgentWriter, f.agent)
		assert.True(t, f.request.JSON, "JSON mode is requested")
	}
}

func TestCompleteJSON_InvalidAnswerIsErrInvalidOutput(t *testing.T) {
	for _, text := range []string{"I cannot do that", `{"title": `, `{"title": 5}`} {
		_, err := llm.CompleteJSON[answer](context.Background(), &fakeCompleter{text: text}, llm.AgentWriter, llm.Request{})
		assert.ErrorIs(t, err, llm.ErrInvalidOutput, text)
	}
}

func TestCompleteJSON_PropagatesProviderErrors(t *testing.T) {
	boom := assert.AnError
	_, err := llm.CompleteJSON[answer](context.Background(), &fakeCompleter{err: boom}, llm.AgentWriter, llm.Request{})
	assert.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, llm.ErrInvalidOutput)
}
