package chat_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

func kinds(msgs []chat.Message) []chat.Kind {
	out := make([]chat.Kind, len(msgs))
	for i, msg := range msgs {
		out[i] = msg.Kind
	}
	return out
}

func TestTheChatAloneGoesFromTheTalkToTheResult(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	// 1. the user says what they want; the LLM asks with cards
	r.LLM.queue(step(m{"action": "choose", "text": "画像と動画のどちらにしますか？", "options": []m{
		{"label": "画像", "detail": "1 枚数円から", "reply": "画像にします"}, {"label": "動画", "detail": "数十円から"},
	}}))
	rep, err := r.Chat.Send(ctx, r.SessID, "夕暮れの狐を作りたい")
	require.NoError(t, err)
	assert.Equal(t, []chat.Kind{chat.KindUser, chat.KindAssistant}, kinds(rep.Messages))
	require.Len(t, rep.Messages[1].Options, 2)
	assert.Equal(t, "画像にします", rep.Messages[1].Options[0].Reply)
	assert.Equal(t, "動画", rep.Messages[1].Options[1].Reply, "a card without a reply says its label")

	// 2. picking a card is a message; the LLM looks at the models, compares and prices two of them
	r.LLM.queue(
		step(m{"action": "list_models", "media": "image"}),
		estimateStep(2, "flux-2", "nano-banana"),
	)
	rep, err = r.Chat.Send(ctx, r.SessID, "画像にします")
	require.NoError(t, err)
	assert.Equal(t, []chat.Kind{chat.KindUser, chat.KindStatus, chat.KindAssistant}, kinds(rep.Messages))
	assert.Contains(t, rep.Messages[1].Text, "画像のモデルを調べました")
	p := proposalOf(t, rep)
	assert.Equal(t, chat.StatePending, p.State)
	assert.Equal(t, 4, p.Estimate.TotalItems, "2 models x 1 prompt x 2")
	assert.InDelta(t, 2*0.03+2*0.0336, p.Estimate.TotalUSD, 1e-9, "the total of the form's estimate")
	require.Len(t, p.Estimate.Breakdown, 2, "a line per model")
	assert.Equal(t, "flux-2", p.Estimate.Breakdown[0].Model)

	// the LLM was shown the same data as the MCP tool (list_generation_models) and then the figures of the estimate
	asked := r.LLM.asked()
	require.Len(t, asked, 3)
	assert.Contains(t, asked[2].User, "list_models: ")
	assert.Contains(t, asked[2].User, `\"id\":\"flux-2\"`)
	assert.Contains(t, asked[2].User, `\"per_image_usd\":0.03`)

	// nothing is generated until the card is pressed
	assert.Equal(t, 0, r.batches(t))
	assert.Empty(t, r.Fake.Calls())

	// 3. the card is pressed: the batch the server priced runs, with the estimate that was shown
	rep, err = r.Chat.Confirm(ctx, r.SessID, p.ID)
	require.NoError(t, err)
	require.Len(t, rep.Messages, 1)
	assert.Equal(t, chat.KindResult, rep.Messages[0].Kind)
	require.Len(t, rep.Changed, 1)
	assert.Equal(t, chat.StateConfirmed, rep.Changed[0].State)
	assert.Equal(t, rep.Messages[0].BatchID, rep.Changed[0].BatchID)

	// 4. the results
	b := r.Wait(rep.Messages[0].BatchID)
	assert.Equal(t, 4, b.Succeeded)
	assert.Len(t, r.Fake.Calls(), 4)
	assert.InDelta(t, p.Estimate.TotalUSD, b.EstimatedUSD, 1e-9)
}

func TestWhatTheLLMAsksForAnEstimateOfIsNotRunUntilTheCardIsPressed(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	// the LLM has no action that runs; one it makes up is an unreadable answer, not a run
	r.LLM.queue(`{"action":"run_generation_test","estimate_id":"x","confirmed_total_usd":0}`, `{"action":"run","confirmed":true}`)
	rep, err := r.Chat.Send(ctx, r.SessID, "もう実行して")
	require.NoError(t, err)
	assert.Equal(t, []chat.Kind{chat.KindUser, chat.KindStatus}, kinds(rep.Messages))
	assert.True(t, rep.Messages[1].Failed)
	assert.Equal(t, 0, r.batches(t))
	assert.Empty(t, r.Fake.Calls())

	// the user typing "実行" does not run an estimate either: only the card does
	r.LLM.queue(estimateStep(1, "flux-2"), step(m{"action": "say", "text": "カードのボタンで実行してください"}))
	rep, err = r.Chat.Send(ctx, r.SessID, "flux-2 で 1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)
	_, err = r.Chat.Send(ctx, r.SessID, "実行")
	require.NoError(t, err)
	assert.Equal(t, 0, r.batches(t), "words never confirm")
	assert.Empty(t, r.Fake.Calls())
	got, err := r.Chat.Proposal(r.SessID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatePending, got.State)
}

func TestACardRunsOnceAndOnlyTheNewestEstimateCanRun(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue(estimateStep(1, "flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "flux-2 で 1 枚")
	require.NoError(t, err)
	first := proposalOf(t, rep)

	r.LLM.queue(estimateStep(2, "nano-banana"))
	rep, err = r.Chat.Send(ctx, r.SessID, "nano-banana で 2 枚に変えて")
	require.NoError(t, err)
	second := proposalOf(t, rep)
	require.Len(t, rep.Changed, 1)
	assert.Equal(t, first.ID, rep.Changed[0].ID)
	assert.Equal(t, chat.StateSuperseded, rep.Changed[0].State)

	_, err = r.Chat.Confirm(ctx, r.SessID, first.ID)
	assert.ErrorIs(t, err, chat.ErrProposalClosed, "an estimate that was replaced does not run")
	assert.Equal(t, 0, r.batches(t))

	_, err = r.Chat.Confirm(ctx, r.SessID, second.ID)
	require.NoError(t, err)
	_, err = r.Chat.Confirm(ctx, r.SessID, second.ID)
	assert.ErrorIs(t, err, chat.ErrProposalClosed, "a second press does not run it again")
	assert.Equal(t, 1, r.batches(t))

	_, err = r.Chat.Confirm(ctx, r.SessID, "nope")
	assert.ErrorIs(t, err, chat.ErrNoProposal)
}

// A press that fails without generating anything puts the card back to pending, unless a newer estimate was made while it ran: then only the newer one may run.
func TestAFailedPressDoesNotReviveACardThatAnEstimateReplacedWhileItRan(t *testing.T) {
	var r *rig
	var second *chat.Proposal
	llmDown := &gentesttest.FakeLLM{Err: errors.New("the translator is down")}
	llmDown.OnCall = func() { // the press of the first card is in its translation: the user asks for another estimate
		if second != nil {
			return
		}
		r.LLM.queue(japaneseEstimateStep("nano-banana"))
		rep, err := r.Chat.Send(context.Background(), r.SessID, "nano-banana で 2 枚に変えて")
		require.NoError(t, err)
		second = proposalOf(t, rep)
	}
	r = newRig(t, gentesttest.WithTranslator(llmDown))
	ctx := context.Background()
	r.LLM.queue(japaneseEstimateStep("flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "flux-2 で 1 枚")
	require.NoError(t, err)
	first := proposalOf(t, rep)

	rep, err = r.Chat.Confirm(ctx, r.SessID, first.ID)
	require.NoError(t, err, "the answer closes the card for the screen instead of leaving its buttons up")
	require.NotNil(t, second, "the second estimate was made while the first press ran")
	require.Len(t, rep.Changed, 1)
	assert.Equal(t, first.ID, rep.Changed[0].ID)
	assert.Equal(t, chat.StateSuperseded, rep.Changed[0].State)
	require.Len(t, rep.Messages, 1)
	assert.True(t, rep.Messages[0].Failed)
	assert.Equal(t, 0, r.batches(t))

	got, err := r.Chat.Proposal(r.SessID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StateSuperseded, got.State, "the first card is not pressable beside the newest one")
	_, err = r.Chat.Confirm(ctx, r.SessID, first.ID)
	assert.ErrorIs(t, err, chat.ErrProposalClosed)
	got, err = r.Chat.Proposal(r.SessID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatePending, got.State, "the newest estimate can still be pressed")
}

// Without a newer estimate the card can be pressed again after a failure.
func TestAFailedPressLeavesTheOnlyCardPending(t *testing.T) {
	llmDown := &gentesttest.FakeLLM{Err: errors.New("the translator is down")}
	r := newRig(t, gentesttest.WithTranslator(llmDown))
	ctx := context.Background()
	r.LLM.queue(japaneseEstimateStep("flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "flux-2 で 1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	_, err = r.Chat.Confirm(ctx, r.SessID, p.ID)
	require.Error(t, err)
	got, err := r.Chat.Proposal(r.SessID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StatePending, got.State)
}

func TestADismissedCardDoesNotRun(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue(estimateStep(1, "flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	rep, err = r.Chat.Dismiss(ctx, r.SessID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StateDismissed, rep.Changed[0].State)
	_, err = r.Chat.Confirm(ctx, r.SessID, p.ID)
	assert.ErrorIs(t, err, chat.ErrProposalClosed)
	assert.Equal(t, 0, r.batches(t))

	// the LLM is told, for the next message
	r.LLM.queue(step(m{"action": "say", "text": "了解です"}))
	_, err = r.Chat.Send(ctx, r.SessID, "やっぱりやめた")
	require.NoError(t, err)
	asked := r.LLM.asked()
	assert.Contains(t, asked[len(asked)-1].User, "turned the confirmation card down")
}

func TestTwoPressesAtOnceRunTheBatchOnce(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue(estimateStep(1, "flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	errs := make(chan error, 2)
	for range 2 {
		go func() { _, err := r.Chat.Confirm(ctx, r.SessID, p.ID); errs <- err }()
	}
	first, second := <-errs, <-errs
	assert.True(t, (first == nil) != (second == nil), "exactly one press runs: %v / %v", first, second)
	assert.ErrorIs(t, errors.Join(first, second), chat.ErrProposalClosed)
	assert.Equal(t, 1, r.batches(t))
}

// The card in a reply is read by the handler without the lock of the conversation, while Confirm / Dismiss change the state of the proposal the session
// keeps: the reply must hold a copy (run with -race), and that copy keeps the state of the moment it was made.
func TestTheCardOfAReplyIsACopyOfWhatTheSessionKeeps(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.LLM.queue(estimateStep(1, "flux-2"))
	rep, err := r.Chat.Send(ctx, r.SessID, "1 枚")
	require.NoError(t, err)
	p := proposalOf(t, rep)

	done := make(chan error, 1)
	go func() { _, err := r.Chat.Dismiss(ctx, r.SessID, p.ID); done <- err }()
	for range 1000 {
		_ = p.State // what the handler does with the card of the reply
	}
	require.NoError(t, <-done)
	assert.Equal(t, chat.StatePending, p.State, "the reply's card is a snapshot: the later dismissal reaches the screen through Reply.Changed")
	got, err := r.Chat.Proposal(r.SessID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, chat.StateDismissed, got.State)
}

func TestAVideoEstimateShowsTheLengthPricedPerSecond(t *testing.T) {
	r := newRig(t)
	r.LLM.queue(step(m{"action": "estimate", "request": m{
		"media": "video", "models": []string{"kling-3.0"}, "prompts": []m{{"prompt": "waves"}}, "resolution": "720p", "duration_seconds": 5,
	}}))
	rep, err := r.Chat.Send(context.Background(), r.SessID, "波の動画")
	require.NoError(t, err)
	p := proposalOf(t, rep)
	require.Len(t, p.Estimate.Breakdown, 1)
	assert.Equal(t, "720p", p.Estimate.Breakdown[0].Resolution)
	assert.InDelta(t, 5.0, p.Estimate.Breakdown[0].Seconds, 1e-9)
	assert.Equal(t, 0, r.batches(t))
}

// A key pasted into the chat is hidden before the message is sent to the LLM or put in the thread (spec/spec.md §1.1); the rest of the message goes as it was.
func TestAPIKeyInAMessageIsNotSentToTheLLM(t *testing.T) {
	r := newRig(t)
	key := "sk-proj-AbCdEfGhIjKlMnOpQrStUvWx0123"
	r.LLM.queue(step(m{"action": "say", "text": "了解です"}))

	rep, err := r.Chat.Send(context.Background(), r.SessID, "このキー "+key+" で夕暮れの狐を作りたい")
	require.NoError(t, err)

	require.Len(t, r.LLM.asked(), 1)
	sent := fmt.Sprintf("%+v", r.LLM.asked()[0])
	assert.NotContains(t, sent, key[len(key)/3:], "the LLM never sees the key")
	assert.Contains(t, sent, secretguard.Hidden)
	assert.Contains(t, sent, "夕暮れの狐を作りたい")
	assert.NotContains(t, rep.Messages[0].Text, key[len(key)/3:], "nor does the thread keep it")
	assert.Contains(t, rep.Messages[0].Text, secretguard.Hidden)
}
