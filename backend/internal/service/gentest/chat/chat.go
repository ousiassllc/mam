// Package chat is the chat of the 単発生成 screen (#1138, spec/architecture/gentest/gentest-chat.md): the user talks to the Reasoning LLM about what
// to make, the LLM looks at the models and prices a batch, and the batch runs only when the user presses the button of the confirmation card.
//
// The tools of the LLM are the service-layer calls the MCP tools of the generation test make (list_generation_models: gentest.Service.ModelsWhere;
// estimate_generation_test: gentest.Service.Estimate; run_generation_test: gentest.Service.Run with the estimate_id and the total that were shown), not copies of
// them, so the limits (gentest.max_items, gentest.max_cost_usd), the refusals and the pricing are the ones of the screen's form and the MCP tools.
//
// What makes the paid step safe:
//   - the LLM has no action that runs a batch (package gentestchat): an estimate it asks for becomes a Proposal, a card the user sees;
//   - Confirm is called by the button of that card only. It runs the request the SERVER priced (the one the Proposal holds), with the estimate_id and the
//     total of that estimate, through gentest.Service.Run, which prices the request again and refuses a changed estimate and one over the limits;
//   - a Proposal is used once, and only the newest pending one of a session can be: a new estimate supersedes the older cards.
//
// A session lives in memory (a chat is a way to fill in a batch: the batches it runs are in the history of the screen), is dropped after
// SessionTTL without use and the oldest is dropped beyond MaxSessions.
package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// Limits of the service.
const (
	// MaxMessageRunes is the longest message of the user.
	MaxMessageRunes = 2000
	// MaxSteps is how many actions the LLM may take to answer one message (the tool calls included).
	MaxSteps = 6
	// MaxTurns is how many turns of a conversation the LLM is shown (the newest).
	MaxTurns = 40
	// MaxSessions is how many conversations are kept.
	MaxSessions = 32
	// SessionTTL is how long a conversation is kept without use.
	SessionTTL = 6 * time.Hour
	// MaxProposals is how many confirmation cards of a conversation are kept: the oldest closed ones are dropped beyond it (a press on one is then ErrNoProposal).
	MaxProposals = 20
)

// Agent is the LLM side (gentestchat.Chat; a scripted one in the tests).
type Agent interface {
	Next(ctx context.Context, in gentestchat.Input) (gentestchat.Step, error)
}

// Deps are what the Service needs.
type Deps struct {
	Gentest *gentest.Service
	Agent   Agent
	// Now is for the tests.
	Now func() time.Time
}

// Service is the chat.
type Service struct {
	d Deps

	mu       sync.Mutex
	sessions map[string]*session
}

// New returns the Service. It is nil-safe on nothing: the screen is left out when the app has no LLM route.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d, sessions: map[string]*session{}}
}

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// NewSessionID is the id of a new conversation (a screen asks for one when it opens). The conversation itself starts with its first message.
func NewSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("gentest chat: no random id: " + err.Error()) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b[:])
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("gentest chat: no random id: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// session is one conversation.
type session struct {
	// send serialises the messages of a conversation (a LLM turn is not interleaved with another one). It is held for a whole Send.
	send sync.Mutex
	// mu guards turns, proposals and order. It is held briefly, never across a LLM call or a Run, so the buttons of a card are not kept waiting while a
	// message is being answered.
	mu        sync.Mutex
	id        string
	turns     []gentestchat.Turn // the newest MaxTurns
	proposals map[string]*Proposal
	order     []string // the ids of proposals, oldest first
	used      time.Time
}

// open returns the conversation, making it when the id is new (the id must be one NewSessionID can make).
func (s *Service) open(id string, create bool) (*session, error) {
	if !sessionIDPattern.MatchString(id) {
		return nil, ErrNoSession
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.d.Now()
	for k, v := range s.sessions {
		if now.Sub(v.used) > SessionTTL {
			delete(s.sessions, k)
		}
	}
	if se, ok := s.sessions[id]; ok {
		se.used = now
		return se, nil
	}
	if !create {
		return nil, ErrNoSession
	}
	for len(s.sessions) >= MaxSessions {
		var oldest string
		for k, v := range s.sessions {
			if oldest == "" || v.used.Before(s.sessions[oldest].used) {
				oldest = k
			}
		}
		delete(s.sessions, oldest)
	}
	se := &session{id: id, proposals: map[string]*Proposal{}, used: now}
	s.sessions[id] = se
	return se, nil
}
