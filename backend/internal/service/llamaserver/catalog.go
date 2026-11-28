package llamaserver

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
)

// LlamaCatalog is the Server.Catalog of llama.cpp: every model of its list (runtimechoices.Views) with its license and whether it is without refusals. Only the model in force can be
// used now (the server loads one model; choosing another stops it, which is the model manager's), so each other model says which one is in force and where to change it (#1235).
func LlamaCatalog(runtimes *localruntime.Service) func(ctx context.Context) []llmlocal.ManagedModel {
	choices := runtimechoices.Service{Runtimes: runtimes}
	return func(ctx context.Context) []llmlocal.ManagedModel {
		views, err := choices.Views(ctx, RuntimeID)
		if err != nil {
			return nil
		}
		active := ""
		for _, v := range views {
			if v.Active {
				active = v.Name
			}
		}
		out := make([]llmlocal.ManagedModel, 0, len(views))
		for _, v := range views {
			m := llmlocal.ManagedModel{
				ID: v.ID, Name: v.Name, Uncensored: v.Uncensored,
				License: modellicense.Info{Commercial: v.LicenseInfo.Commercial, LicenseName: v.LicenseInfo.Name, SourceURL: v.LicenseInfo.URL, Notice: v.LicenseInfo.Notice},
			}
			if !v.Active {
				m.Reason = "今の llama.cpp のモデルは「" + active + "」です。モデルマネージャーの「ローカル LLM」の llama.cpp で、このモデルを選んで「重みを導入」してください"
			}
			out = append(out, m)
		}
		return out
	}
}
