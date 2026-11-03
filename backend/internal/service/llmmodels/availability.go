package llmmodels

import (
	"context"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// availability is whether a provider whose list fell back for this reason can be used: without a key or a sign-in it cannot (#1013); a provider that did
// not answer has its key, so the models are still offered.
func (f Fallback) availability() modelfilter.Availability {
	switch f {
	case FallbackNoKey:
		return modelfilter.Unusable(modelfilter.ReasonNoKey, f.Message()+"（設定の「API キー」で登録します）", modelfilter.RouteOf(modelfilter.ReasonNoKey, modelfilter.SourceBuiltin))
	case FallbackNotSignedIn:
		return modelfilter.Unusable(modelfilter.ReasonNotSignedIn, f.Message()+"（設定の「ChatGPT でログイン」からログインします）", modelfilter.RouteOf(modelfilter.ReasonNotSignedIn, modelfilter.SourceChatGPT))
	}
	return modelfilter.Usable()
}

// ProviderNames are the providers the assignment can name, in the order the screens list them (llm.Providers()).
func ProviderNames() []string {
	out := make([]string, 0, len(llm.Providers()))
	for _, p := range llm.Providers() {
		out = append(out, string(p))
	}
	return out
}

// Availability is whether the provider can be used now: an API key is saved, or the subscription is signed in. It reads the key store and the sign-in only (no
// network), so it is cheap enough for every render of the assignment screen. ErrUnknownProvider for a provider that is not one of llm.Providers().
func (s *Service) Availability(provider string) (modelfilter.Availability, error) {
	p := llm.Provider(provider)
	if !slices.Contains(llm.Providers(), p) {
		return modelfilter.Availability{}, fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
	_, fallback := s.account(p)
	return fallback.availability(), nil
}

// Candidate is one model a picker of the LLM assignment offers, with whether it can be chosen now.
type Candidate struct {
	Provider string
	Model    Model
	// Source and Fallback are those of the provider's list (SourceAPI: the account's own models; SourceDefault: the candidates mam ships).
	Source       Source
	Fallback     Fallback
	Availability modelfilter.Availability
}

// Item is the model as the filter reads it (modelfilter.Condition.Match): an LLM, served by its provider through an API key or a subscription.
func (c Candidate) Item() modelfilter.Item {
	name := c.Model.Name
	if name == "" {
		name = c.Model.ID
	}
	return c.Availability.Item(modelfilter.Item{
		ID: c.Model.ID, Name: name, Kinds: []modelfilter.Kind{modelfilter.KindLLM}, Installed: true, Origin: modelfilter.OriginAPI, Provider: c.Provider,
	})
}

// Candidates lists the models of every provider (llm.Providers() order) that pass the condition. The picker of the assignment starts at
// modelfilter.Default(): the models of a provider without an API key or a sign-in are left out, and its "使えないものも表示" (AvailableOnly off) lists them with
// the reason (Candidate.Availability). A provider that cannot be used is not asked for its list (the candidates mam ships stand in). The
// settings screen's API key cards do not use this: they list every provider.
func (s *Service) Candidates(ctx context.Context, c modelfilter.Condition) ([]Candidate, error) {
	var out []Candidate
	for _, p := range llm.Providers() {
		l, err := s.List(ctx, string(p))
		if err != nil {
			return nil, err
		}
		for _, m := range l.Models {
			out = append(out, Candidate{Provider: l.Provider, Model: m, Source: l.Source, Fallback: l.Fallback, Availability: l.Availability})
		}
	}
	return modelfilter.Filter(c, out, Candidate.Item), nil
}

// Target is one provider and model an Agent's route names.
type Target struct {
	Agent string
	// Role is "primary" or "fallback".
	Role, Provider, Model string
}

// Warning says that a saved route names a provider that cannot be used now.
type Warning struct {
	Target
	Availability modelfilter.Availability
	// Message is the sentence for the user: which Agent and role, the provider, why, and (in Availability.Route) the screen that fixes it.
	Message string
}

var roleWords = map[string]string{"primary": "プライマリ", "fallback": "代替"}

// Warnings checks the saved routes against the providers: every target whose provider has no API key or no sign-in is a warning, in the order given (#1013).
// The assignment screen shows them; the routes are kept (saving never refuses for it: a key may be added later). A provider mam does not know is skipped:
// the save refused it already.
func (s *Service) Warnings(targets []Target) []Warning {
	var out []Warning
	for _, t := range targets {
		a, err := s.Availability(t.Provider)
		if err != nil || a.Available {
			continue
		}
		out = append(out, Warning{
			Target: t, Availability: a,
			Message: fmt.Sprintf("%s の%sは %s を指していますが、使えません: %s", t.Agent, roleWords[t.Role], t.Provider, a.Message),
		})
	}
	return out
}
