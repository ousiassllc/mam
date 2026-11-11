package servicetest

import (
	"context"
	"slices"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
)

// The canned answers of the planning Agents of FakeReasoner.
const (
	FakeConcept = `{"title":"朝の一滴","logline":"肌が目覚める朝","concept":"朝の光と美容液","tone":"上品","structure":[{"name":"フック","description":"光","duration_seconds":5}]}`
	FakeScript  = `{"title":"脚本","scenes":[{"title":"朝","location":"寝室","description":"光が差す"},{"title":"使う","description":"美容液を手に取る"}]}`
	FakePlan    = `{"scenes":[
		{"title":"朝","location":"寝室","shots":[{"objective":"窓の光","duration_seconds":5},{"objective":"目覚め","duration_seconds":5}]},
		{"title":"使う","shots":[{"objective":"美容液","duration_seconds":5}]}]}`
	FakeBoard = `{"scenes":[{"shots":[{"description":"窓辺の光","framing":"ワイド"},{"description":"目を開ける"}]},{"shots":[{"description":"手のひらの美容液"}]}]}`
)

// FakeReasoner stands in for the Reasoning LLM Adapter: no external API is called. It answers per Agent with canned JSON, can fail an Agent on demand and records every
// call in order.
type FakeReasoner struct {
	mu    sync.Mutex
	calls []llm.Agent
	users map[llm.Agent][]string
	fail  map[llm.Agent]error
	// answers replaces the canned text of an Agent (SetAnswer).
	answers map[llm.Agent]string
}

// NewFakeReasoner is a FakeReasoner that answers every planning Agent and fails none.
func NewFakeReasoner() *FakeReasoner {
	return &FakeReasoner{users: map[llm.Agent][]string{}, fail: map[llm.Agent]error{}, answers: map[llm.Agent]string{}}
}

// Complete answers the Agent with its canned text, or with the failure set for it.
func (f *FakeReasoner) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, agent)
	f.users[agent] = append(f.users[agent], req.User)
	if err := f.fail[agent]; err != nil {
		return llm.Response{}, err
	}
	text := map[llm.Agent]string{
		llm.AgentProducer: FakeConcept, llm.AgentWriter: FakeScript, llm.AgentDirector: FakePlan, llm.AgentStoryboard: FakeBoard,
	}[agent]
	if answer, ok := f.answers[agent]; ok {
		text = answer
	}
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// Agents are the Agents called so far, in order.
func (f *FakeReasoner) Agents() []llm.Agent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Count is how many times the Agent was called.
func (f *FakeReasoner) Count(agent llm.Agent) int {
	n := 0
	for _, a := range f.Agents() {
		if a == agent {
			n++
		}
	}
	return n
}

// LastUser is the user prompt of the last call of the Agent ("" when it was not called).
func (f *FakeReasoner) LastUser(agent llm.Agent) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.users[agent]
	if len(u) == 0 {
		return ""
	}
	return u[len(u)-1]
}

// SetFailure makes the Agent fail with err from now on (nil: answer again).
func (f *FakeReasoner) SetFailure(agent llm.Agent, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[agent] = err
}

// SetAnswer makes the Agent answer with text instead of its canned one.
func (f *FakeReasoner) SetAnswer(agent llm.Agent, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers[agent] = text
}

// Prompts are the user prompts the Agent was called with, in order.
func (f *FakeReasoner) Prompts(agent llm.Agent) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.users[agent])
}
