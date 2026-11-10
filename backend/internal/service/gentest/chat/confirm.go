package chat

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// Confirm is the button of a confirmation card: it runs the batch of the proposal with the estimate id and the total that were shown on the card (the
// ones the server kept, not anything the browser sent), through gentest.Service.Run, which prices the batch again and refuses it when the estimate is not
// the same any more or is over gentest.max_cost_usd, and when it is over gentest.max_items. Nothing is generated before that check passes.
//
// The reply holds the changed card and, when the batch was queued, the message that shows its results. A refusal that says the confirmation is no good (a
// changed estimate, a limit) closes the proposal as stale and is a message of the reply, so the user is told in the thread; the LLM is told too, for the next
// message. Other failures (the translation of the prompts, a fault) are returned and leave the proposal pending: nothing was generated, the user may press again (but if a newer estimate was made while the press ran, the proposal is superseded instead: only the newest card can be pressed).
// ErrProposalClosed means it was used, dismissed or replaced already.
func (s *Service) Confirm(ctx context.Context, sessionID, proposalID string) (Reply, error) {
	se, err := s.open(sessionID, false)
	if err != nil {
		return Reply{}, err
	}
	p, err := take(se, proposalID)
	if err != nil {
		return Reply{}, err
	}
	// The session is not locked while the batch is queued (the translation of the prompts is a LLM call): the state "running" refuses a second press.
	ref, runErr := s.d.Gentest.Run(ctx, p.Request, gentest.Confirmation{EstimateID: p.Estimate.ID, TotalUSD: p.Estimate.TotalUSD})

	se.mu.Lock()
	defer se.mu.Unlock()
	var (
		conflict *gterr.ConflictError
		invalid  *gterr.InvalidError
	)
	switch {
	case runErr == nil:
		p.State, p.BatchID = StateConfirmed, ref.BatchID
		se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleTool, Text: "The user confirmed the card. The batch was started: " + itoa(ref.TotalItems) + " results; the results are shown in the chat by the app."})
		return Reply{Changed: []Proposal{*p}, Messages: []Message{{ID: newID(), Kind: KindResult, BatchID: ref.BatchID}}}, nil
	case errors.As(runErr, &conflict):
		return s.stale(se, p, conflict.Message), nil
	case errors.As(runErr, &invalid): // the request cannot run now (a limit, a model that is gone)
		return s.stale(se, p, invalid.Message), nil
	}
	p.State = StatePending // nothing was generated: the card can be pressed again, unless a newer estimate was made meanwhile
	if p.replaced {
		p.State = StateSuperseded
	}
	return Reply{}, runErr
}

// take marks a pending proposal as running, under the lock of the conversation; the caller then runs it without the lock.
func take(se *session, id string) (*Proposal, error) {
	se.mu.Lock()
	defer se.mu.Unlock()
	p, ok := se.proposals[id]
	if !ok {
		return nil, ErrNoProposal
	}
	if p.State != StatePending {
		return nil, ErrProposalClosed
	}
	p.State = StateRunning
	return p, nil
}

// stale closes a proposal the service refused and tells the user and the LLM why.
func (s *Service) stale(se *session, p *Proposal, why string) Reply {
	p.State, p.Reason = StateStale, why
	se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleTool, Text: "The user pressed the confirmation card but the app refused to run it, nothing was generated: " + why + " Estimate again if the user still wants it."})
	return Reply{
		Changed:  []Proposal{*p},
		Messages: []Message{{ID: newID(), Kind: KindStatus, Failed: true, Text: "実行しませんでした: " + why + "。続けるには、もう一度見積りを取ってください（「もう一度見積りして」と送れます）"}},
	}
}

// Dismiss turns a pending proposal down ("やめる"): nothing runs, and the LLM is told.
func (s *Service) Dismiss(_ context.Context, sessionID, proposalID string) (Reply, error) {
	se, err := s.open(sessionID, false)
	if err != nil {
		return Reply{}, err
	}
	se.mu.Lock()
	defer se.mu.Unlock()
	p, ok := se.proposals[proposalID]
	if !ok {
		return Reply{}, ErrNoProposal
	}
	if p.State != StatePending {
		return Reply{}, ErrProposalClosed
	}
	p.State = StateDismissed
	se.turns = append(se.turns, gentestchat.Turn{Role: gentestchat.RoleTool, Text: "The user turned the confirmation card down. Nothing was generated."})
	return Reply{Changed: []Proposal{*p}}, nil
}

// Proposal returns a copy of a proposal of the conversation (the card as it is now).
func (s *Service) Proposal(sessionID, proposalID string) (Proposal, error) {
	se, err := s.open(sessionID, false)
	if err != nil {
		return Proposal{}, err
	}
	se.mu.Lock()
	defer se.mu.Unlock()
	p, ok := se.proposals[proposalID]
	if !ok {
		return Proposal{}, ErrNoProposal
	}
	return *p, nil
}
