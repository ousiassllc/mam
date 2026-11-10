package chat

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// Errors of the service the screen words itself.
var (
	// ErrNoSession means the conversation is not kept any more (the app restarted, or it was unused for SessionTTL).
	ErrNoSession = errors.New("gentest chat: no such conversation")
	// ErrNoProposal means the card names no proposal of the conversation.
	ErrNoProposal = errors.New("gentest chat: no such proposal")
	// ErrProposalClosed means the proposal was used, dismissed or superseded: it cannot be confirmed.
	ErrProposalClosed = errors.New("gentest chat: the proposal is closed")
	// ErrBadMessage means the message is empty or too long.
	ErrBadMessage = errors.New("gentest chat: the message is empty or too long")
)

// Kind is what a Message is.
type Kind string

// The kinds of a message.
const (
	KindUser      Kind = "user"
	KindAssistant Kind = "assistant"
	// KindStatus is a line of the app (the models were looked at; an estimate was refused; the LLM failed), not words of the user or the LLM.
	KindStatus Kind = "status"
	// KindResult is a batch that the user confirmed and that runs; BatchID names it.
	KindResult Kind = "result"
)

// Message is one entry of the thread the screen draws.
type Message struct {
	ID   string
	Kind Kind
	Text string
	// Options are the cards of a question (KindAssistant).
	Options []gentestchat.Option
	// Proposal is the confirmation card that follows the text (KindAssistant).
	Proposal *Proposal
	// BatchID is the batch of a KindResult.
	BatchID string
	// Failed says a KindStatus line is a failure (the screen draws it as one); LLMKey says it is the lack of a key of the LLM (the screen links the assignment).
	Failed bool
	LLMKey bool
}

// State of a proposal.
type State string

// The states of a Proposal.
const (
	// StatePending can be confirmed.
	StatePending State = "pending"
	// StateRunning is being confirmed now (a second press is refused).
	StateRunning State = "running"
	// StateConfirmed was run: BatchID names the batch.
	StateConfirmed State = "confirmed"
	// StateDismissed was turned down by the user.
	StateDismissed State = "dismissed"
	// StateSuperseded was replaced by a newer estimate of the conversation.
	StateSuperseded State = "superseded"
	// StateStale was refused when confirmed: the prices or the models changed since the estimate, or it is over the limits.
	StateStale State = "stale"
)

// Proposal is an estimate the user is asked to confirm. The request and the figures are the server's: the screen sends the id of the card, nothing
// else, so what runs is exactly what was shown.
type Proposal struct {
	ID       string
	State    State
	Request  gentest.Request
	Estimate gentest.Estimate
	// BatchID is the batch of a confirmed proposal; Reason is why a stale one was refused.
	BatchID string
	Reason  string
	// replaced says a newer estimate was made while this proposal was running: if the press then fails, it is superseded, not pending again (only the newest card can be pressed).
	replaced bool
}

// Reply is what a call answers: the messages to append to the thread, and the proposals that changed state outside of them (the screen
// draws those again where they are).
type Reply struct {
	Messages []Message
	Changed  []Proposal
}
