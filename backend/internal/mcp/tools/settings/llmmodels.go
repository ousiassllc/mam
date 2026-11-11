package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// ModelInfo is one model of a provider.
type ModelInfo struct {
	ID   string `json:"id" jsonschema:"set_llm_assignment の model に渡す名前"`
	Name string `json:"name,omitempty" jsonschema:"プロバイダーが返した表示名（あるときだけ）"`
}

// ProviderModels is the models of one provider and where they come from.
type ProviderModels struct {
	Provider string      `json:"provider"`
	Source   string      `json:"source" jsonschema:"api（プロバイダーが返した一覧。そのアカウント・API キーで使えるもの）または default（mam が持つ既定候補）"`
	Reason   string      `json:"reason,omitempty" jsonschema:"source が default の理由（API キーなし・ログインなし・取得に失敗）"`
	Models   []ModelInfo `json:"models"`
	// Availability: false while the provider has no API key or no sign-in (#1013); the models are then mam's candidates, listed so that the
	// choice can be made ahead of the key.
	ProviderAvailability
}

// ProviderAvailability is whether a provider of the LLM assignment can be used now, with why not and the screen that fixes it.
type ProviderAvailability struct {
	Available          bool   `json:"available" jsonschema:"このプロバイダーを今使えるか。API キーが登録されている、またはサブスクでログイン済みなら true"`
	UnavailableMessage string `json:"unavailable_message,omitempty" jsonschema:"使えない理由（API キー未登録・ChatGPT 未ログイン）"`
	SettingsRoute      string `json:"settings_route,omitempty" jsonschema:"直す画面の道すじ（例 /settings/api-keys）"`
}

func providerAvailabilityOf(a modelfilter.Availability) ProviderAvailability {
	return ProviderAvailability{Available: a.Available, UnavailableMessage: a.Message, SettingsRoute: a.Route}
}

// ListModelsInput names the provider to list.
type ListModelsInput struct {
	Provider string `json:"provider,omitempty" jsonschema:"get_global_settings の llm.providers のどれか。省略すると全プロバイダー"`
	// AvailableOnly leaves out the providers that cannot be used now (#1013), as the picker of the screen does at first.
	AvailableOnly bool `json:"available_only,omitempty" jsonschema:"true なら、今使えないプロバイダー（API キー未登録・未ログイン）を除く。省略は全部を理由つきで返す"`
}

// ListModelsOutput is the models, one entry for each provider asked.
type ListModelsOutput struct {
	Providers []ProviderModels `json:"providers"`
}

func providerModelsOf(l llmmodels.List) ProviderModels {
	out := ProviderModels{
		Provider: l.Provider, Source: string(l.Source), Reason: l.Fallback.Message(), Models: make([]ModelInfo, len(l.Models)),
		ProviderAvailability: providerAvailabilityOf(l.Availability),
	}
	for i, m := range l.Models {
		out.Models[i] = ModelInfo{ID: m.ID, Name: m.Name}
	}
	return out
}

func (h handlers) listModels(ctx context.Context, in ListModelsInput) (ListModelsOutput, error) {
	models, llmSvc := h.r.Settings.LLMModels, h.r.Settings.LLM
	if models == nil || llmSvc == nil {
		return ListModelsOutput{}, Unavailable("LLM のモデル一覧")
	}
	providers := llmSvc.View().Providers
	if in.Provider != "" {
		providers = []string{in.Provider}
	}
	out := ListModelsOutput{Providers: make([]ProviderModels, 0, len(providers))}
	for _, p := range providers {
		l, err := models.List(ctx, p)
		if errors.Is(err, llmmodels.ErrUnknownProvider) {
			return ListModelsOutput{}, invalidInput("provider", fmt.Sprintf("%q は未対応のプロバイダーです。選べる値は get_global_settings の llm.providers です", p))
		}
		if err != nil {
			return ListModelsOutput{}, err
		}
		if in.AvailableOnly && !l.Availability.Available {
			continue
		}
		out.Providers = append(out.Providers, providerModelsOf(l))
	}
	return out, nil
}

// ModelWarning is a saved model that the provider's list does not have: the save was not refused, but the model may not
// work.
type ModelWarning struct {
	Kind     string `json:"kind" jsonschema:"unlisted_model"`
	Agent    string `json:"agent"`
	Role     string `json:"role" jsonschema:"primary または fallback"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Message  string `json:"message"`
}

// SetLLMOutput is the saved assignment and the warnings about the models just saved. A model that is in no list is a
// warning, never a refusal: lists change faster than the app.
type SetLLMOutput struct {
	LLMSection
	Warnings []ModelWarning `json:"warnings" jsonschema:"保存したモデルのうち、そのプロバイダーの一覧にないもの（保存はされている。list_llm_models で確かめる）"`
}

// unlistedWarnings checks the models of the saved routes against the lists of their providers (kept for ten minutes, so a
// save does not ask the providers again). Without the list service, or for a provider it cannot list, there is no warning.
func (h handlers) unlistedWarnings(ctx context.Context, routes []RouteInput) []ModelWarning {
	warnings := []ModelWarning{}
	models := h.r.Settings.LLMModels
	if models == nil {
		return warnings
	}
	for _, r := range routes {
		for _, t := range []struct {
			role   string
			target Target
		}{{"primary", r.Primary}, {"fallback", r.Fallback}} {
			model := modelTrimmed(t.target).Model
			l, err := models.List(ctx, t.target.Provider)
			if err != nil || l.Has(model) {
				continue
			}
			warnings = append(warnings, ModelWarning{
				Kind: "unlisted_model", Agent: r.Agent, Role: t.role, Provider: t.target.Provider, Model: model, Message: unlistedMessage(model, l),
			})
		}
	}
	return warnings
}

func unlistedMessage(model string, l llmmodels.List) string {
	if l.Fallback == llmmodels.FallbackLocalDown {
		return fmt.Sprintf("%q を確かめられません（%s）。保存はしました。サーバーを起動してから list_llm_models（provider: local）で選べる名前を確かめてください", model, l.Fallback.Message())
	}
	if l.Source == llmmodels.SourceAPI {
		return fmt.Sprintf("%q は %s の一覧にありません。使えないかもしれません（保存はしました）。list_llm_models で選べる名前を確かめられます", model, l.Provider)
	}
	return fmt.Sprintf("%q は mam の既定候補にありません（%s: %s）。新しいモデルなら使えることもありますが、使えないかもしれません（保存はしました）", model, l.Provider, l.Fallback.Message())
}
