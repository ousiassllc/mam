package gentesttest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

// The kinds of call the FakeMatchLLM tells apart.
const (
	CallDescribe = "describe"
	CallCompare  = "compare"
	CallRevise   = "revise"
)

// MatchCall is one call the FakeMatchLLM got.
type MatchCall struct {
	Kind  string
	Agent llm.Agent
	// User is the JSON the LLM was given as text; Images the number of pictures that came with it.
	User   string
	Images int
}

// FakeMatchLLM is the Reasoning LLM of a prompt match: it writes "prompt 1" for the reference, answers every comparison with the next
// of Scores (the last one again when they run out) and fixes a prompt by writing ", fix <n>" after it. Err fails the calls of a
// kind; Before runs at the start of every call (a test holds a call there). Usage is what the answer of a kind says it used (a kind
// that is not in it reports nothing: the tokens are unknown); Answered is the target the answers come from (anthropic / fake-llm,
// which has no price, when it is empty). Text answers a kind with that text instead of the JSON (a call that was paid for and whose answer
// cannot be used).
type FakeMatchLLM struct {
	mu       sync.Mutex
	calls    []MatchCall
	next     int
	fixes    int
	Scores   []int
	Err      map[string]error
	Before   func(kind string)
	Usage    map[string]llm.Usage
	Answered llm.Target
	Text     map[string]string
}

// Calls returns the calls so far.
func (f *FakeMatchLLM) Calls() []MatchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]MatchCall(nil), f.calls...)
}

// Count is how many calls of the kind there were.
func (f *FakeMatchLLM) Count(kind string) int {
	n := 0
	for _, c := range f.Calls() {
		if c.Kind == kind {
			n++
		}
	}
	return n
}

// Complete answers like the LLM of the prompt match would.
func (f *FakeMatchLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	kind := CallDescribe
	switch {
	case strings.HasPrefix(req.System, "You compare"):
		kind = CallCompare
	case strings.HasPrefix(req.System, "You fix"):
		kind = CallRevise
	}
	if f.Before != nil {
		f.Before(kind)
	}
	f.mu.Lock()
	f.calls = append(f.calls, MatchCall{Kind: kind, Agent: agent, User: req.User, Images: len(req.Images)})
	err := f.Err[kind]
	var text any
	switch kind {
	case CallDescribe:
		text = map[string]string{"prompt": "prompt 1"}
	case CallCompare:
		score := 50
		if len(f.Scores) > 0 {
			score = f.Scores[min(f.next, len(f.Scores)-1)]
			f.next++
		}
		text = map[string]any{"score": score, "differences": []string{fmt.Sprintf("差 %d", f.next)}}
	default:
		var in struct{ Prompt string }
		_ = json.Unmarshal([]byte(req.User), &in)
		f.fixes++
		text = map[string]string{"prompt": fmt.Sprintf("%s, fix %d", in.Prompt, f.fixes), "change": fmt.Sprintf("直した %d", f.fixes)}
	}
	f.mu.Unlock()
	if err != nil {
		return llm.Response{}, err
	}
	raw, _ := json.Marshal(text)
	if t, ok := f.Text[kind]; ok {
		raw = []byte(t)
	}
	res := llm.Response{Text: string(raw), Provider: llm.ProviderAnthropic, Model: "fake-llm"}
	if f.Answered != (llm.Target{}) {
		res.Provider, res.Model = f.Answered.Provider, f.Answered.Model
	}
	if u, ok := f.Usage[kind]; ok {
		res.Usage = &u
	}
	return res, nil
}

// UseMatch gives the Service the prompt match through f, and returns it. The targets the estimate names (Target) have no price.
func (r *Rig) UseMatch(f *FakeMatchLLM) *match.Service { return r.UseMatchWith(f, Target, Target) }

// UseMatchWith is UseMatch with the targets of the two routes: the one that writes the prompts and the one that compares.
func (r *Rig) UseMatchWith(f *FakeMatchLLM, create, judge llm.Target) *match.Service {
	return r.UseMatcher(imagematch.New(f, create, judge))
}

// UseMatcher gives the Service the prompt match through m (a Matcher a test has set up, for example with a face engine).
func (r *Rig) UseMatcher(m *imagematch.Matcher) *match.Service {
	return match.New(match.Deps{Gentest: r.Svc, Matcher: m})
}
