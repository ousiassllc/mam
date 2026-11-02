package llmmodels

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

// FromChatGPT is the ChatGPT accounts as the two ChatGPT providers see them: "chatgpt-subscription" is the official
// sign-in (GET /v1/models of api.openai.com) and "chatgpt-codex" the Codex login (model/list of the Codex app-server, which
// holds the Codex login's tokens itself: mam never reads them).
func FromChatGPT(svc *chatgpt.Service) ChatGPT { return chatgptAccounts{svc} }

type chatgptAccounts struct{ svc *chatgpt.Service }

func (s chatgptAccounts) Account(p llm.Provider) string {
	switch p {
	case llm.ProviderChatGPTSubscription:
		ov, err := s.svc.Overview()
		if err != nil || !ov.Official.SignedIn || ov.Official.NeedsRelogin {
			return ""
		}
		return "official:" + ov.Official.Email
	case llm.ProviderChatGPTCodex:
		if a := s.svc.Codex().Status().Account; a.SignedIn {
			return "codex:" + a.Email
		}
	}
	return ""
}

func (s chatgptAccounts) Models(ctx context.Context, p llm.Provider) ([]Model, error) {
	var out []Model
	switch p {
	case llm.ProviderChatGPTSubscription:
		models, err := s.svc.Models(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			out = append(out, Model{ID: m.Slug, Name: m.DisplayName})
		}
	case llm.ProviderChatGPTCodex:
		models, err := s.svc.Codex().Models(ctx)
		if err != nil {
			return nil, err
		}
		for _, m := range models {
			out = append(out, Model{ID: m.Slug, Name: m.DisplayName})
		}
	default:
		return nil, fmt.Errorf("llmmodels: %q is not a ChatGPT provider", p)
	}
	return out, nil
}
