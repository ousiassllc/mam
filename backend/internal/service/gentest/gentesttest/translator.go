package gentesttest

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/prompttranslate"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
)

// FakeLLM is the Reasoning LLM of a Rig that translates: it answers the texts it is given with Say (by default "EN: " and the
// text), or fails with Err. Asked is every call it got (the agent and the texts).
type FakeLLM struct {
	mu    sync.Mutex
	asked []Asked
	// Say is the English of a text; nil gives "EN: " + text.
	Say func(text string) string
	// Err, when set, is the failure of every call.
	Err error
}

// Asked is one call of the FakeLLM.
type Asked struct {
	Agent llm.Agent
	Texts []string
}

// Complete answers {"texts": [...]} with the translation of the texts of the request.
func (f *FakeLLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	var in struct {
		Texts []string `json:"texts"`
	}
	_ = json.Unmarshal([]byte(req.User), &in)
	f.mu.Lock()
	f.asked = append(f.asked, Asked{Agent: agent, Texts: slices.Clone(in.Texts)})
	say, err := f.Say, f.Err
	f.mu.Unlock()
	if err != nil {
		return llm.Response{}, err
	}
	for i, t := range in.Texts {
		if say != nil {
			in.Texts[i] = say(t)
		} else {
			in.Texts[i] = "EN: " + t
		}
	}
	out, _ := json.Marshal(in)
	return llm.Response{Text: string(out), Provider: llm.ProviderAnthropic, Model: "fake-llm"}, nil
}

// Asked returns the calls so far.
func (f *FakeLLM) Asked() []Asked {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Asked(nil), f.asked...)
}

// Target is the provider and model the estimate names for the FakeLLM.
var Target = llm.Target{Provider: llm.ProviderAnthropic, Model: "fake-llm"}

// WithTranslator gives the Service the translation through f.
func WithTranslator(f *FakeLLM) Option {
	return func(_ *Rig, d *gentest.Deps) { d.Translator = prompttranslate.New(f, Target) }
}
