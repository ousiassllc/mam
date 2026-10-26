package settings

import (
	"context"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
)

// Target is one provider and one model name.
type Target struct {
	Provider string `json:"provider" jsonschema:"LLM のプロバイダー。選べる値は get_global_settings の llm.providers"`
	Model    string `json:"model" jsonschema:"モデル名（128 バイトまで）"`
}

// AgentRoute is the assignment of one Agent: what is saved and what the program ships.
type AgentRoute struct {
	Agent           string `json:"agent" jsonschema:"set_llm_assignment に渡す Agent 名"`
	Label           string `json:"label"`
	Unused          bool   `json:"unused" jsonschema:"今のコードでは使われない Agent（割り当ては保存できる）"`
	Primary         Target `json:"primary"`
	Fallback        Target `json:"fallback" jsonschema:"主が失敗したときに切り替える先"`
	DefaultPrimary  Target `json:"default_primary"`
	DefaultFallback Target `json:"default_fallback"`
}

// LLMSection is the "LLM の割り当て" section.
type LLMSection struct {
	Available      bool         `json:"available" jsonschema:"false ならこのアプリには LLM の割り当てがない"`
	Pinned         bool         `json:"pinned" jsonschema:"起動オプションで設定ファイルが固定されていて、変更できない"`
	Problem        string       `json:"problem,omitempty" jsonschema:"設定ファイルを読めない理由。あるときは既定を示し、保存はできない"`
	Providers      []string     `json:"providers"`
	MaxModelLength int          `json:"max_model_length"`
	Agents         []AgentRoute `json:"agents"`
}

func targetOf(t llmassign.Target) Target { return Target(t) }

func llmSection(svc *llmassign.Service) LLMSection {
	v := svc.View()
	out := LLMSection{
		Available: true, Pinned: v.Pinned, Problem: v.Problem, Providers: v.Providers,
		MaxModelLength: llmassign.MaxModelBytes, Agents: make([]AgentRoute, 0, len(v.Entries)),
	}
	for _, e := range v.Entries {
		out.Agents = append(out.Agents, AgentRoute{
			Agent: e.Agent, Label: e.Label, Unused: e.Unused,
			Primary: targetOf(e.Route.Primary), Fallback: targetOf(e.Route.Fallback),
			DefaultPrimary: targetOf(e.Default.Primary), DefaultFallback: targetOf(e.Default.Fallback),
		})
	}
	return out
}

// RouteInput is the new assignment of one Agent.
type RouteInput struct {
	Agent    string `json:"agent" jsonschema:"get_global_settings の llm.agents[].agent"`
	Primary  Target `json:"primary"`
	Fallback Target `json:"fallback"`
}

// SetLLMInput lists the Agents to change. An Agent that is not listed keeps what is saved.
type SetLLMInput struct {
	Routes []RouteInput `json:"routes" jsonschema:"変える Agent の割り当て（1 つ以上）"`
}

func llmInvalid(err error) (string, bool) {
	var invalid *llmassign.InvalidError
	if errors.As(err, &invalid) {
		return invalid.Message, true
	}
	return "", false
}

func (h handlers) setLLM(_ context.Context, in SetLLMInput) (LLMSection, error) {
	svc := h.r.Settings.LLM
	if svc == nil {
		return LLMSection{}, unavailable("LLM の割り当て")
	}
	if len(in.Routes) == 0 {
		return LLMSection{}, invalidInput("routes", "変える Agent を 1 つ以上指定してください")
	}
	routes := make(map[string]llmassign.Route, len(in.Routes))
	for _, r := range in.Routes {
		routes[r.Agent] = llmassign.Route{Primary: modelTrimmed(r.Primary), Fallback: modelTrimmed(r.Fallback)}
	}
	if err := svc.Save(routes); err != nil {
		return LLMSection{}, mapSaveError(err, llmInvalid, llmassign.ErrPinned)
	}
	return llmSection(svc), nil
}

// modelTrimmed is the target as the screen's form reads it: the model name without surrounding space.
func modelTrimmed(t Target) llmassign.Target {
	return llmassign.Target{Provider: t.Provider, Model: strings.TrimSpace(t.Model)}
}

func (h handlers) resetLLM(_ context.Context, _ Input) (LLMSection, error) {
	svc := h.r.Settings.LLM
	if svc == nil {
		return LLMSection{}, unavailable("LLM の割り当て")
	}
	if err := svc.Reset(); err != nil {
		return LLMSection{}, mapSaveError(err, llmInvalid, llmassign.ErrPinned)
	}
	return llmSection(svc), nil
}
