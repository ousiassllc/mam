package chat_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

func TestAnEstimateTheServiceRefusesIsToldToTheLLM(t *testing.T) {
	r := newRig(t)
	r.LLM.queue(
		step(m{"action": "estimate", "request": m{"media": "image", "models": []string{"no-such-model"}, "prompts": []m{{"prompt": "x"}}}}),
		step(m{"action": "list_models", "media": "image", "inputs": []string{"nonsense"}}),
		step(m{"action": "say", "text": "モデルの名前を確認します"}),
	)
	rep, err := r.Chat.Send(context.Background(), r.SessID, "x")
	require.NoError(t, err)
	assert.Equal(t, chat.KindAssistant, rep.Messages[len(rep.Messages)-1].Kind)
	asked := r.LLM.asked()
	assert.Contains(t, asked[1].User, "estimate refused: ")
	assert.Contains(t, asked[2].User, "list_models refused: ", "a condition the models list refuses is told, not a failure")
	assert.Equal(t, 0, r.batches(t))
}

func TestAnUnreadableAnswerIsAskedAgainOnceAndTheFailureIsAMessage(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue("I think you want a fox.", step(m{"action": "say", "text": "狐ですね"}))
	rep, err := r.Chat.Send(ctx, r.SessID, "狐")
	require.NoError(t, err)
	assert.Equal(t, "狐ですね", rep.Messages[1].Text)
	assert.Contains(t, r.LLM.asked()[1].User, "not one of the allowed JSON actions")

	r.LLM.queue("nope", "still nope")
	rep, err = r.Chat.Send(ctx, r.SessID, "もう一度")
	require.NoError(t, err)
	assert.Equal(t, chat.KindStatus, rep.Messages[1].Kind)
	assert.True(t, rep.Messages[1].Failed)
}

func TestAFailureOfTheLLMIsAMessageThatSaysWhat(t *testing.T) {
	r := newRig(t)
	for name, tc := range map[string]struct {
		err  error
		want string
		key  bool
	}{
		"no key":  {llm.ErrNoAPIKey, "API キー", true},
		"breaker": {llm.ErrCircuitOpen, "一時的に使えません", false},
		"timeout": {llm.ErrTimeout, "時間内に返りませんでした", false},
		"other":   {errors.New("boom"), "LLM の割り当て", false},
	} {
		r.LLM.err = tc.err
		rep, err := r.Chat.Send(context.Background(), r.SessID, "狐")
		require.NoError(t, err, name)
		last := rep.Messages[len(rep.Messages)-1]
		assert.Equal(t, chat.KindStatus, last.Kind, name)
		assert.True(t, last.Failed, name)
		assert.Equal(t, tc.key, last.LLMKey, name)
		assert.Contains(t, last.Text, tc.want, name)
		assert.NotContains(t, last.Text, "boom", name+": the raw error stays in the log")
	}
}

func TestAMessageAndASessionAreChecked(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for name, text := range map[string]string{"empty": "  \n", "long": strings.Repeat("あ", chat.MaxMessageRunes+1)} {
		_, err := r.Chat.Send(ctx, r.SessID, text)
		assert.ErrorIs(t, err, chat.ErrBadMessage, name)
	}
	_, err := r.Chat.Send(ctx, "../etc", "x")
	assert.ErrorIs(t, err, chat.ErrNoSession, "an id that NewSessionID cannot make")
	_, err = r.Chat.Confirm(ctx, chat.NewSessionID(), "p")
	assert.ErrorIs(t, err, chat.ErrNoSession, "a card of a conversation that is gone (the app restarted) runs nothing")
	_, err = r.Chat.Dismiss(ctx, chat.NewSessionID(), "p")
	assert.ErrorIs(t, err, chat.ErrNoSession)
	assert.Empty(t, r.LLM.asked(), "no message reached the LLM")
}

func TestConversationsExpireAndAreBounded(t *testing.T) {
	g := gentesttest.New(t)
	s := &script{}
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	svc := chat.New(chat.Deps{Gentest: g.Svc, Agent: gentestAgent(s), Now: func() time.Time { return now }})
	ctx := context.Background()

	s.queue(estimateStep(1, "flux-2"))
	id := chat.NewSessionID()
	rep, err := svc.Send(ctx, id, "1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	now = now.Add(chat.SessionTTL + time.Minute)
	_, err = svc.Confirm(ctx, id, p.ID)
	assert.ErrorIs(t, err, chat.ErrNoSession, "a conversation that was not used for the TTL is gone, and its card with it")
	s.queue(step(m{"action": "say", "text": "はい"}))
	_, err = svc.Send(ctx, id, "もう一度")
	assert.NoError(t, err, "a message to a conversation that is gone starts a new one under the same id (only an id of a wrong shape is refused: 410)")
	assert.Equal(t, 0, len(mustBatches(t, g)))

	first := chat.NewSessionID()
	for i := range chat.MaxSessions + 1 {
		s.queue(step(m{"action": "say", "text": "はい"}))
		id := first
		if i > 0 {
			id = chat.NewSessionID()
		}
		_, err := svc.Send(ctx, id, "こんにちは")
		require.NoError(t, err)
		now = now.Add(time.Second)
	}
	_, err = svc.Dismiss(ctx, first, "p")
	assert.ErrorIs(t, err, chat.ErrNoSession, "the oldest conversation was dropped beyond MaxSessions")
}

func mustBatches(t *testing.T, g *gentesttest.Rig) []gentest.BatchStatus {
	t.Helper()
	out, err := g.Svc.Batches(context.Background(), 50)
	require.NoError(t, err)
	return out
}
