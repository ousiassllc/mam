package ollamapull

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
)

// Choices is every model of the list as the picker of the LLM assignment shows it (#1235): the models fetched can be chosen, the others say they have to be fetched first, in the
// model manager's page "ローカル LLM". It reads the manifests on disk and the pulls in progress, and never asks the server (the picker draws it at every opening).
func (s *Service) Choices(ctx context.Context) []llmlocal.ManagedModel {
	rv, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil {
		return nil
	}
	out := make([]llmlocal.ManagedModel, 0, len(s.Models))
	for _, m := range s.Models {
		cm := llmlocal.ManagedModel{ID: m.Name, Name: m.DisplayName, Uncensored: m.Uncensored, License: m.License}
		if rv.Support.Supported && rv.Installed {
			switch {
			case s.activeJob(TargetPrefix+m.Name) != "":
				cm.Reason = "取得中です。終わるまでお待ちください"
			case s.CheckManifest(m) == CheckMissing:
				cm.Reason = "まだ取得していません。モデルマネージャーの「ローカル LLM」の「Ollama のモデル」で取得してください"
			}
		}
		out = append(out, cm)
	}
	return out
}
