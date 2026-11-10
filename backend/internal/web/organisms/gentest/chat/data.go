// Package chat draws the chat of the single generation screen (paths.GenerationTestChat, #1138): the thread (the words of the user and of the LLM, the cards
// that offer options, the confirmation card of an estimate, the block that shows the results of a batch) and the box to write in. The handlers build
// these structs; nothing here calls a service.
package chat

import (
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/resulttile"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// DOM ids the handlers' HTMX targets name.
const (
	ThreadID   = "gtc-thread"
	ComposerID = "gtc-composer"
	// Polling is the hx-trigger of a results block that asks for itself again while its batch runs.
	Polling = "every 2s"
)

// MaxMessageRunes is the longest message of the box (the service refuses a longer one: chat.MaxMessageRunes, which the handlers' test holds equal).
const MaxMessageRunes = 2000

// Form field names (the handlers read the same names).
const (
	FieldSession = "session"
	FieldContent = "content"
)

// Kind is what a message is.
type Kind string

// The kinds of a message.
const (
	KindUser      Kind = "user"
	KindAssistant Kind = "assistant"
	KindStatus    Kind = "status"
	KindResult    Kind = "result"
)

// Panel is the chat: the id of the conversation it posts to.
type Panel struct {
	SessionID string
	// Unavailable says the app has no LLM for the chat (the Prompt Planner has no route in the assignment): the page says so and has no box.
	Unavailable bool
}

// Message is one entry of the thread.
type Message struct {
	ID   string
	Kind Kind
	Text string
	// Options are the cards of a question; picking one sends its Reply.
	Options  []Option
	Proposal *Proposal
	Result   *Result
	// Failed draws a status line as a failure; SettingsHref, when set, adds the link to the assignment of the LLM.
	Failed       bool
	SettingsHref string
}

// Option is a card of a question.
type Option struct {
	Label  string
	Detail string
	Reply  string
}

// Line is one model of an estimate.
type Line struct {
	Label    string
	Items    int
	Unit     string
	Subtotal string
}

// KV is a fact of the batch the card asks the user to confirm.
type KV struct {
	Key   string
	Value string
}

// Proposal is the confirmation card of an estimate. Open is true while the buttons can be pressed; State says how a closed card was closed.
type Proposal struct {
	ID        string
	SessionID string
	// State is the state of the proposal (chat.State*): the card is open for "pending" and "running".
	State  string
	Open   bool
	Items  int
	Total  string
	Facts  []KV
	Prompt []PromptText
	Lines  []Line
	// Unknown is the note about the models without a price; Warnings are the warnings of the estimate.
	Unknown  string
	Warnings []string
	// StateText says a closed card in words; Reason is why a stale one was refused.
	StateText string
	Reason    string
	// Swap draws the card as an out-of-band replacement of the card that is in the thread.
	Swap bool
}

// PromptText is a prompt of the batch with its negative prompt.
type PromptText struct {
	Text     string
	Negative string
}

// Result is the block that shows the results of a batch of the chat.
type Result struct {
	BatchID  string
	State    string
	StateTxt string
	Tone     atoms.BadgeTone
	Done     int
	Total    int
	Percent  int
	Finished bool
	Counts   string
	Estimate string
	Error    string
	Tiles    []Tile
}

// Tile is a result: the picture and who made it.
type Tile struct {
	Card  resulttile.Card
	Model string
}

// ID is the DOM id of the confirmation card of a proposal.
func ID(proposal string) string { return "gtc-p-" + proposal }

// ResultID is the DOM id of the results block of a batch.
func ResultID(batch string) string { return "gtc-r-" + batch }

func messagesPath() string { return paths.GenerationTestChat("messages") }

func confirmPath(id string) string { return paths.GenerationTestChat("proposals", id, "confirm") }

func dismissPath(id string) string { return paths.GenerationTestChat("proposals", id, "dismiss") }

func batchPath(id string) string { return paths.GenerationTestChat("batches", id) }
