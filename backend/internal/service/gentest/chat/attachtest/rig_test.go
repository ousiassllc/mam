package attachtest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/chat"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// script is a Reasoning LLM that answers with the steps a test queues and keeps what it was asked.
type script struct {
	mu    sync.Mutex
	steps []string
	reqs  []llm.Request
}

func (s *script) Complete(_ context.Context, _ llm.Agent, req llm.Request) (llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
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

type m = map[string]any

func step(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// estimate asks for a video batch of the model; audio and image say which attached file it uses.
func estimate(model string, audio, image bool) string {
	return step(m{"action": "estimate", "text": "見積りです", "request": m{
		"media": "video", "models": []string{model}, "prompts": []m{{"prompt": "she says hello"}}, "resolution": "720p", "duration_seconds": 5, "audio": audio, "image": image,
	}})
}

type rig struct {
	*gentesttest.Rig
	Chat *chat.Service
	LLM  *script
	Sess string
	ctx  context.Context
}

func newRig(t *testing.T) *rig {
	t.Helper()
	g := gentesttest.New(t, gentesttest.WithInputModels())
	s := &script{}
	return &rig{Rig: g, LLM: s, Chat: chat.New(chat.Deps{Gentest: g.Svc, Agent: gentestchat.New(s)}), Sess: chat.NewSessionID(), ctx: context.Background()}
}

func (r *rig) attach(t *testing.T, kind, name string, data []byte) chat.Reply {
	t.Helper()
	rep, err := r.Chat.Attach(r.ctx, r.Sess, kind, name, data)
	require.NoError(t, err)
	return rep
}

func (r *rig) send(t *testing.T, text string) chat.Reply {
	t.Helper()
	rep, err := r.Chat.Send(r.ctx, r.Sess, text)
	require.NoError(t, err)
	return rep
}

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

func lastTool(t *testing.T, s *script) string {
	t.Helper()
	asked := s.asked()
	require.NotEmpty(t, asked)
	return asked[len(asked)-1].User
}

// cause is the Japanese cause of a refusal that was stopped as the common error.
func cause(t *testing.T, err error) string {
	t.Helper()
	e, ok := apperr.As(err)
	require.True(t, ok, "not a common error: %v", err)
	return e.View(apperr.LangJa).Cause
}

var s2v, t2v = string(gentesttest.S2VModel), string(gentesttest.T2VModel)
