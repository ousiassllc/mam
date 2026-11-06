package overview

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// localLLM is the line of the page "ローカル LLM": the server the calls of the provider "local" go to and where it stands, in words. "使うサーバー: この PC の llama.cpp（導入済み・停止中）",
// "使うサーバー: 別のサーバー（http://localhost:1234）", "使うサーバー: 未設定" (the address is the default and no server of mam is in use). The address is shown as the settings keep it, without a user name or a
// query; an API key is never part of it. The state is ready (the server can be used), attention (a server of mam that is not installed, not supported or did not start), installing (an install
// of a server of mam runs, whichever is in use) or empty (nothing chosen).
func localLLM(ctx context.Context, svc *llmlocal.Service, rts *localruntime.Service) (string, string) {
	st := svc.Status(ctx)
	text, state := "使うサーバー: "+st.Use.Name(), view.StateReady
	switch {
	case st.Use.Managed():
		text += "（" + st.Word() + "）"
	case st.Default:
		text, state = "使うサーバー: 未設定", view.StateEmpty
	default:
		text += "（" + st.URL + "）"
	}
	switch {
	case installingLLM(ctx, rts):
		return text + "・導入中", view.StateInstalling
	case st.Problem() != "":
		return text, view.StateAttention
	}
	return text, state
}

// installingLLM says that an install of a runtime of a language model is running.
func installingLLM(ctx context.Context, svc *localruntime.Service) bool {
	if svc == nil {
		return false
	}
	for _, r := range svc.List(ctx) {
		if r.Purpose == catalog.PurposeLLM && r.InstallJob != "" {
			return true
		}
	}
	return false
}
