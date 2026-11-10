package chat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// script is a Reasoning LLM that answers the chat with the steps a test queues, in order, and keeps what it was asked.
type script struct {
	mu    sync.Mutex
	steps []string
	err   error
	reqs  []llm.Request
}

func (s *script) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
	if agent != gentestchat.Agent {
		return llm.Response{}, fmt.Errorf("the chat asked the wrong agent: %s", agent)
	}
	if s.err != nil {
		return llm.Response{}, s.err
	}
	if len(s.steps) == 0 {
		return llm.Response{}, fmt.Errorf("the script has no more steps (call %d)", len(s.reqs))
	}
	text := s.steps[0]
	s.steps = s.steps[1:]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake-llm"}, nil
}

func (s *script) queue(steps ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, steps...)
}

func (s *script) asked() []llm.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]llm.Request(nil), s.reqs...)
}

// step writes one action of the LLM.
func step(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

type m = map[string]any

// estimateStep asks for an estimate of an image batch of the models, one prompt, count pictures per model.
func estimateStep(count int, models ...string) string {
	return step(m{"action": "estimate", "text": "見積りです", "request": m{
		"media": "image", "models": models, "prompts": []m{{"prompt": "a fox at dusk", "negative_prompt": "blurry"}}, "count": count,
	}})
}

type rig struct {
	*gentesttest.Rig
	Chat   *chat.Service
	LLM    *script
	SessID string
}

func newRig(t *testing.T, opts ...gentesttest.Option) *rig {
	t.Helper()
	g := gentesttest.New(t, opts...)
	s := &script{}
	return &rig{Rig: g, LLM: s, Chat: chat.New(chat.Deps{Gentest: g.Svc, Agent: gentestchat.New(s)}), SessID: chat.NewSessionID()}
}

// proposalOf is the confirmation card of the reply.
func proposalOf(t *testing.T, rep chat.Reply) *chat.Proposal {
	t.Helper()
	for _, msg := range rep.Messages {
		if msg.Proposal != nil {
			return msg.Proposal
		}
	}
	require.Fail(t, "no confirmation card in the reply", "%+v", rep.Messages)
	return nil
}

// batches is how many batches the generation test has: a run that was refused leaves none.
func (r *rig) batches(t *testing.T) int {
	t.Helper()
	b, err := r.Svc.Batches(context.Background(), 50)
	require.NoError(t, err)
	return len(b)
}

func gentestAgent(s *script) chat.Agent { return gentestchat.New(s) }
