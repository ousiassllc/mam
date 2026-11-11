package chat

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
)

// note adds a turn to the conversation; the caller holds se.mu.
func (se *session) note(role gentestchat.Role, text string) {
	se.turns = append(se.turns, gentestchat.Turn{Role: role, Text: text})
	se.trim()
}

// trim keeps the conversation bounded (it lives up to SessionTTL): the turns the LLM is shown, and the newest MaxProposals cards (the open ones are never
// dropped). The caller holds se.mu.
func (se *session) trim() {
	if len(se.turns) > MaxTurns {
		se.turns = slices.Clone(se.turns[len(se.turns)-MaxTurns:]) // a copy, so that the dropped turns are not kept by the old array
	}
	if len(se.order) <= MaxProposals {
		return
	}
	drop := len(se.order) - MaxProposals
	kept := se.order[:0:0]
	for _, id := range se.order {
		if p := se.proposals[id]; drop > 0 && p.State != StatePending && p.State != StateRunning {
			delete(se.proposals, id)
			drop--
			continue
		}
		kept = append(kept, id)
	}
	se.order = kept
}

// exchange is what one message adds to the conversation. It is kept apart until the message is answered, so a message that fails (a cancelled request, a
// fault) leaves nothing of itself: the thread does not show it either, and the LLM must not be left with a question the user was never answered.
type exchange struct {
	se  *session
	own []gentestchat.Turn
}

func (x *exchange) add(role gentestchat.Role, text string) {
	x.own = append(x.own, gentestchat.Turn{Role: role, Text: text})
}

// shown is the conversation the LLM is shown: the newest turns of the conversation and of this message.
func (x *exchange) shown() []gentestchat.Turn {
	x.se.mu.Lock()
	defer x.se.mu.Unlock()
	return recent(append(slices.Clone(x.se.turns), x.own...))
}

// commit puts the turns of the answered message into the conversation.
func (x *exchange) commit() {
	x.se.mu.Lock()
	defer x.se.mu.Unlock()
	for _, t := range x.own {
		x.se.note(t.Role, t.Text)
	}
}
