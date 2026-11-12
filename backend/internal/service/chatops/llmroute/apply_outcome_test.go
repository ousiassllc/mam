package llmroute

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
)

func TestWhatTheServiceRefusesIsToldAndNothingIsSaved(t *testing.T) {
	f := newFake()
	a := Args{Agent: "director", Provider: "anthropic", Model: "claude-opus-4-5"}

	f.view.Pinned = true
	_, err := ask(t, f, a)
	assert.Contains(t, refusal(t, err), "--llm-config")
	f.view.Pinned = false

	f.view.Problem = "invalid character"
	_, err = ask(t, f, a)
	assert.Contains(t, refusal(t, err), "読み込めない")
	f.view.Problem = ""

	card, err := ask(t, f, a)
	require.NoError(t, err)
	f.saveErr = &llmassign.InvalidError{Message: "Director（Scene / Shot の計画） のプライマリ: モデル名が長すぎます"}
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	assert.Contains(t, refusal(t, err), "モデル名が長すぎます")
	f.saveErr = llmassign.ErrPinned
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	assert.Contains(t, refusal(t, err), "--llm-config")
	f.saveErr = errors.New("disk")
	_, err = f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	var r *chatops.Refusal
	assert.False(t, errors.As(err, &r))
	assert.Empty(t, f.saved)
}

func TestWithoutALiveClientTheCardSaysItWaitsForARestartAndTheAnswerSaysWhatPends(t *testing.T) {
	f := newFake()
	f.view.Live = false
	f.view.Entries = f.view.Entries[:3]

	_, err := ask(t, f, Args{Agent: "final_qc", Model: "gpt-5-mini", Provider: "openai"})
	assert.Contains(t, refusal(t, err), "どの Agent", "final_qc is not among the entries of this view")

	card, err := ask(t, f, Args{Agent: "director", Provider: "anthropic", Model: "claude-opus-4-5"})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "次にアプリを起動したときから使われます")
	assert.NotContains(t, card.Text, "すぐ使われます")

	f.view.Live, f.view.Pending = true, []string{"timeout"}
	res, err := f.handler().Apply(context.Background(), domain.Project{}, *card.Op)
	require.NoError(t, err)
	assert.Contains(t, res.Text, "再起動するまで効かない設定があります: timeout")
}

func TestAnAgentTheCodeDoesNotUseIsSaidSo(t *testing.T) {
	f := newFake()

	card, err := ask(t, f, Args{Agent: "final_qc", Provider: "openai", Model: "gpt-5-mini"})

	require.NoError(t, err)
	assert.Contains(t, card.Text, "今のコードでは使われません")
}
