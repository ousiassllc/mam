package settings

import (
	"context"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
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
	Available bool     `json:"available" jsonschema:"false ならこのアプリには LLM の割り当てがない"`
	Pinned    bool     `json:"pinned" jsonschema:"起動オプションで設定ファイルが固定されていて、変更できない"`
	Problem   string   `json:"problem,omitempty" jsonschema:"設定ファイルを読めない理由。あるときは既定を示し、保存はできない"`
	Providers []string `json:"providers"`
	// LocalServer is the setting of the provider "local" (the local OpenAI-compatible LLM server); set_local_llm_server changes it.
	LocalServer    LocalServer  `json:"local_server" jsonschema:"プロバイダー local（Ollama・LM Studio・llama.cpp server）のアドレスと状態。set_local_llm_server で変える"`
	MaxModelLength int          `json:"max_model_length"`
	Agents         []AgentRoute `json:"agents"`
	// ProviderStates say, for each of Providers, whether it can be used now (#1013): an API key is saved, or the subscription is signed in.
	ProviderStates []ProviderState `json:"provider_states" jsonschema:"providers の各プロバイダーが今使えるか（available）と、使えない理由・直す画面の道すじ。API キー未登録・未ログインのプロバイダーのモデルは選べない"`
	// RouteWarnings are the saved routes (primary and fallback) that point at a provider that cannot be used now.
	RouteWarnings  []RouteWarning `json:"route_warnings" jsonschema:"今の割り当てが、使えないプロバイダー（API キー未登録・未ログイン）を指しているもの。保存はできるが、その Agent の呼び出しはキーを登録するまで失敗する"`
	AppliesAtOnce  bool           `json:"applies_at_once" jsonschema:"true なら、保存した割り当ては次の LLM 呼び出しからすぐ効く（再起動は要らない）"`
	RestartPending []string       `json:"restart_pending" jsonschema:"保存したが、再起動するまで効かない設定（llm.json を手で書き換えた base_url・タイムアウト・リトライ・サーキットブレーカー）。無ければ空。restart_app で反映する"`
}

// ProviderState is whether one provider can be used now.
type ProviderState struct {
	Provider string `json:"provider"`
	ProviderAvailability
}

// RouteWarning is a saved route whose provider cannot be used now.
type RouteWarning struct {
	Agent         string `json:"agent"`
	Role          string `json:"role" jsonschema:"primary または fallback"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Message       string `json:"message"`
	SettingsRoute string `json:"settings_route,omitempty" jsonschema:"直す画面の道すじ"`
}

func targetOf(t llmassign.Target) Target { return Target(t) }

// llmSection is the section; models may be nil (the service is not there): the states and warnings are then left empty.
func llmSection(svc *llmassign.Service, models *llmmodels.Service, local *llmlocal.Service) LLMSection {
	v := svc.View()
	out := LLMSection{
		Available: true, Pinned: v.Pinned, Problem: v.Problem, Providers: v.Providers,
		MaxModelLength: llmassign.MaxModelBytes, Agents: make([]AgentRoute, 0, len(v.Entries)),
		LocalServer: localServerOf(local), AppliesAtOnce: v.Live, RestartPending: append([]string{}, v.Pending...), ProviderStates: []ProviderState{}, RouteWarnings: []RouteWarning{},
	}
	if models != nil {
		for _, p := range v.Providers {
			if a, err := models.Availability(p); err == nil {
				out.ProviderStates = append(out.ProviderStates, ProviderState{Provider: p, ProviderAvailability: providerAvailabilityOf(a)})
			}
		}
		var targets []llmmodels.Target
		for _, e := range v.Entries {
			targets = append(targets,
				llmmodels.Target{Agent: e.Agent, Role: "primary", Provider: e.Route.Primary.Provider, Model: e.Route.Primary.Model},
				llmmodels.Target{Agent: e.Agent, Role: "fallback", Provider: e.Route.Fallback.Provider, Model: e.Route.Fallback.Model})
		}
		for _, w := range models.Warnings(targets) {
			out.RouteWarnings = append(out.RouteWarnings, RouteWarning{
				Agent: w.Agent, Role: w.Role, Provider: w.Provider, Model: w.Model, Message: w.Message, SettingsRoute: w.Availability.Route,
			})
		}
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

func (h handlers) setLLM(ctx context.Context, in SetLLMInput) (SetLLMOutput, error) {
	svc := h.r.Settings.LLM
	if svc == nil {
		return SetLLMOutput{}, Unavailable("LLM の割り当て")
	}
	if len(in.Routes) == 0 {
		return SetLLMOutput{}, invalidInput("routes", "変える Agent を 1 つ以上指定してください")
	}
	routes := make(map[string]llmassign.Route, len(in.Routes))
	for _, r := range in.Routes {
		routes[r.Agent] = llmassign.Route{Primary: modelTrimmed(r.Primary), Fallback: modelTrimmed(r.Fallback)}
	}
	if err := svc.Save(routes); err != nil {
		return SetLLMOutput{}, MapSaveError(err, llmInvalid, llmassign.ErrPinned)
	}
	return SetLLMOutput{LLMSection: llmSection(svc, h.r.Settings.LLMModels, h.r.Settings.LLMLocal), Warnings: h.unlistedWarnings(ctx, in.Routes)}, nil
}

// modelTrimmed is the target as the screen's form reads it: the model name without surrounding space.
func modelTrimmed(t Target) llmassign.Target {
	return llmassign.Target{Provider: t.Provider, Model: strings.TrimSpace(t.Model)}
}

func (h handlers) resetLLM(_ context.Context, _ Input) (LLMSection, error) {
	svc := h.r.Settings.LLM
	if svc == nil {
		return LLMSection{}, Unavailable("LLM の割り当て")
	}
	if err := svc.Reset(); err != nil {
		return LLMSection{}, MapSaveError(err, llmInvalid, llmassign.ErrPinned)
	}
	return llmSection(svc, h.r.Settings.LLMModels, h.r.Settings.LLMLocal), nil
}
