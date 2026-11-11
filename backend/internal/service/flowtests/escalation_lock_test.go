package flowtests_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

const opHold commandinterpreter.OpType = "test_hold"

func init() {
	commandinterpreter.Register(opHold, commandinterpreter.Spec{Clean: func(op commandinterpreter.Operation) (commandinterpreter.Operation, bool) { return op, true }})
}

// holdHandler is an operation whose first Apply waits until released: the answer to its card is "being carried out" for as long as the test wants. A later Apply (an answer
// that was not kept out) returns at once, so that a missing lock fails the test instead of hanging it.
type holdHandler struct {
	calls   *atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (holdHandler) Question(context.Context, domain.Project, commandinterpreter.Operation) (chatops.Card, error) {
	return chatops.Card{}, nil
}

func (h holdHandler) Apply(context.Context, domain.Project, commandinterpreter.Operation) (chatops.Result, error) {
	if h.calls.Add(1) == 1 {
		h.entered <- struct{}{}
		<-h.release
	}
	return chatops.Result{Text: "done"}, nil
}

// A button of an Escalation carries out an operation: an answer that arrives while another one to the same card is being carried out is a conflict, not a second run.
func TestChat_AnEscalationCardTakesOneAnswerAtATime(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p := f.createProject(t, "美容液のCMを作りたい")
	h := holdHandler{calls: new(atomic.Int32), entered: make(chan struct{}), release: make(chan struct{})}
	f.svc.ChatOps().Handle(opHold, h)
	action, err := json.Marshal(commandinterpreter.Operation{Type: opHold})
	require.NoError(t, err)
	body := domain.CardBody{Text: "どうしますか", Options: []string{"実行する"}, OptionActions: []json.RawMessage{action}}
	require.NoError(t, f.svc.Chat.RaiseEscalationCard(ctx, p.ID, body, ""))
	card := escalationCard(t, f, p.ID)
	first := make(chan error, 1)
	go func() {
		_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "実行する", ReplyToMessageID: card.ID})
		first <- err
	}()
	select {
	case <-h.entered: // the first answer is being carried out
	case <-time.After(5 * time.Second):
		t.Fatal("the first answer never reached the handler")
	}

	_, err = f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{Content: "実行する", ReplyToMessageID: card.ID})

	assert.ErrorIs(t, err, service.ErrConflict)
	assert.EqualValues(t, 1, h.calls.Load(), "the operation ran once")
	close(h.release)
	require.NoError(t, <-first)
}
