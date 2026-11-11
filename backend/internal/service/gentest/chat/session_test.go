package chat_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// gate holds the LLM of the chat until the test lets it go, and says when a call is waiting.
type gate struct {
	inner   chat.Agent
	mu      sync.Mutex
	hold    chan struct{}
	waiting chan struct{}
}

func (g *gate) Next(ctx context.Context, in gentestchat.Input) (gentestchat.Step, error) {
	g.mu.Lock()
	hold, waiting := g.hold, g.waiting
	g.mu.Unlock()
	if hold != nil {
		waiting <- struct{}{}
		<-hold
	}
	return g.inner.Next(ctx, in)
}

func (g *gate) holdNext() (release func(), waiting <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.hold, g.waiting = make(chan struct{}), make(chan struct{}, 1)
	hold := g.hold
	var once sync.Once
	return func() { once.Do(func() { close(hold) }) }, g.waiting
}

// While a message is being answered (a LLM call can take long), the buttons of the cards of the same conversation work: they are not queued behind the answer.
func TestTheButtonsOfACardWorkWhileAMessageIsBeingAnswered(t *testing.T) {
	r := newRig(t)
	g := &gate{inner: gentestAgent(r.LLM)}
	c := chat.New(chat.Deps{Gentest: r.Svc, Agent: g})
	ctx := context.Background()
	r.LLM.queue(estimateStep(1, "flux-2"))
	rep, err := c.Send(ctx, r.SessID, "1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	release, waiting := g.holdNext()
	t.Cleanup(release)
	r.LLM.queue(step(m{"action": "say", "text": "了解です"}))
	sent := make(chan error, 1)
	go func() { _, err := c.Send(ctx, r.SessID, "ところで"); sent <- err }()
	<-waiting // the LLM call of the second message is under way

	pressed := make(chan error, 1)
	go func() { _, err := c.Confirm(ctx, r.SessID, p.ID); pressed <- err }()
	select {
	case err := <-pressed:
		require.NoError(t, err, "the card was pressed while the message was being answered")
	case <-time.After(10 * time.Second):
		require.Fail(t, "the press waited for the answer to the message")
	}
	release()
	require.NoError(t, <-sent)
	assert.Equal(t, 1, r.batches(t))
}

// A message that fails (a request that was cancelled) leaves nothing of itself in the conversation: the thread does not show it and the LLM must not read a
// question that was never answered.
func TestAMessageThatFailsLeavesNothingInTheConversation(t *testing.T) {
	r := newRig(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	r.LLM.queue(step(m{"action": "say", "text": "答えです"}))
	_, err := r.Chat.Send(cancelled, r.SessID, "届かなかった発言")
	require.ErrorIs(t, err, context.Canceled)

	r.LLM.queue(step(m{"action": "say", "text": "了解です"}))
	_, err = r.Chat.Send(context.Background(), r.SessID, "次の発言")
	require.NoError(t, err)
	asked := r.LLM.asked()
	require.Len(t, asked, 2)
	assert.NotContains(t, asked[1].User, "届かなかった発言")
	assert.Contains(t, asked[1].User, "次の発言")
}

// The LLM that cannot answer is a line of the reply, and the message the user said stays in the conversation.
func TestAMessageTheLLMCouldNotAnswerStaysInTheConversation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.err = errors.New("down")
	_, err := r.Chat.Send(ctx, r.SessID, "つながらない間の発言")
	require.NoError(t, err)
	r.LLM.err = nil
	r.LLM.queue(step(m{"action": "say", "text": "了解です"}))
	_, err = r.Chat.Send(ctx, r.SessID, "もう一度")
	require.NoError(t, err)
	asked := r.LLM.asked()
	assert.Contains(t, asked[len(asked)-1].User, "つながらない間の発言")
}

// A conversation lives up to SessionTTL, so what it keeps is bounded: the turns the LLM is shown, and the newest MaxProposals cards.
func TestAConversationKeepsBoundedTurnsAndCards(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	var first *chat.Proposal
	var last *chat.Proposal
	for i := range chat.MaxProposals + 5 {
		r.LLM.queue(estimateStep(1, "flux-2"))
		rep, err := r.Chat.Send(ctx, r.SessID, fmt.Sprintf("見積り %d", i))
		require.NoError(t, err)
		last = proposalOf(t, rep)
		if first == nil {
			first = last
		}
	}
	_, err := r.Chat.Proposal(r.SessID, first.ID)
	assert.ErrorIs(t, err, chat.ErrNoProposal, "the oldest closed card was dropped")
	got, err := r.Chat.Proposal(r.SessID, last.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatePending, got.State, "the newest card, which is open, is kept")

	r.LLM.queue(step(m{"action": "say", "text": "了解です"}))
	_, err = r.Chat.Send(ctx, r.SessID, "最後の発言")
	require.NoError(t, err)
	asked := r.LLM.asked()
	shown := asked[len(asked)-1].User
	assert.Contains(t, shown, "最後の発言")
	assert.NotContains(t, shown, "見積り 0\"", "the oldest turns are no longer kept")
}

func TestADismissedCardCannotBeDismissedAgain(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue(estimateStep(1, "flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)
	_, err = r.Chat.Dismiss(ctx, r.SessID, p.ID)
	require.NoError(t, err)
	_, err = r.Chat.Dismiss(ctx, r.SessID, p.ID)
	assert.ErrorIs(t, err, chat.ErrProposalClosed)
	_, err = r.Chat.Dismiss(ctx, r.SessID, "nope")
	assert.ErrorIs(t, err, chat.ErrNoProposal)
}

// The LLM is told why a press of the first card did nothing when a newer estimate replaced it meanwhile (the tool turn the next message reads).
func TestTheLLMIsToldThatTheReplacedCardWasPressed(t *testing.T) {
	var r *rig
	llmDown := &gentesttest.FakeLLM{Err: errors.New("the translator is down")}
	llmDown.OnCall = func() {
		r.LLM.queue(japaneseEstimateStep("nano-banana"))
		_, err := r.Chat.Send(context.Background(), r.SessID, "nano-banana に変えて")
		require.NoError(t, err)
		llmDown.OnCall = nil
	}
	r = newRig(t, gentesttest.WithTranslator(llmDown))
	ctx := context.Background()
	r.LLM.queue(japaneseEstimateStep("flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "flux-2 で 1 枚")
	require.NoError(t, err)
	_, err = r.Chat.Confirm(ctx, r.SessID, proposalOf(t, rep).ID)
	require.NoError(t, err)

	r.LLM.queue(step(m{"action": "say", "text": "了解です"}))
	_, err = r.Chat.Send(ctx, r.SessID, "どうなった？")
	require.NoError(t, err)
	asked := r.LLM.asked()
	assert.Contains(t, asked[len(asked)-1].User, "had been replaced by a newer estimate meanwhile")
}

// A list_models the service refuses is not "0 models looked at": the line says it was refused.
func TestARefusedModelListIsNotReportedAsAnEmptyOne(t *testing.T) {
	r := newRig(t)
	r.LLM.queue(step(m{"action": "list_models", "media": "image", "inputs": []string{"nonsense"}}), step(m{"action": "say", "text": "条件を直します"}))
	rep, err := r.Chat.Send(context.Background(), r.SessID, "探して")
	require.NoError(t, err)
	require.Equal(t, []chat.Kind{chat.KindUser, chat.KindStatus, chat.KindAssistant}, kinds(rep.Messages))
	assert.Contains(t, rep.Messages[1].Text, "調べられませんでした")
	assert.NotContains(t, rep.Messages[1].Text, "0 件")
}

// The LLM that gave an unreadable answer is told what was wrong with it, not only that something was.
func TestTheLLMIsToldWhyItsAnswerWasNotReadable(t *testing.T) {
	r := newRig(t)
	r.LLM.queue(`{"action":"say"}`, step(m{"action": "say", "text": "了解です"}))
	_, err := r.Chat.Send(context.Background(), r.SessID, "こんにちは")
	require.NoError(t, err)
	assert.Contains(t, r.LLM.asked()[1].User, "say without text")
}
