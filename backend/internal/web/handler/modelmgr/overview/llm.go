package overview

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
)

// localLLM is the line of the page "ローカル LLM": the server the calls of the provider "local" go to and where it stands, in words. "使うサーバー: この PC の llama.cpp（導入済み・停止中）",
// "使うサーバー: 別のサーバー（http://localhost:1234）", "使うサーバー: 未設定" (the address is the default and no server of mam is in use). The address is shown as the settings keep it, without a user name or a
// query; an API key is never part of it. The state is ready (the server can be used), attention (a server of mam that is not installed, not supported or did not start), installing (an install
// of a server of mam runs, or a model is being fetched, whichever is in use) or empty (nothing chosen). When the Ollama of mam is installed the line goes on with how many of its models are
// fetched ("・Ollama のモデル 1 / 3 件", #1105).
func localLLM(ctx context.Context, svc *llmlocal.Service, rts *localruntime.Service, ol *ollamapull.Service) (string, string) {
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
	count, pulling := ollamaModels(ctx, ol)
	text += count
	switch {
	case installingLLM(ctx, rts):
		return text + "・導入中", view.StateInstalling
	case pulling:
		return text + "・取得中", view.StateInstalling
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

// ollamaModels is the tail of the line about the models of the Ollama of mam ("" while it is not installed or this build has none) and whether one of them is being fetched.
func ollamaModels(ctx context.Context, svc *ollamapull.Service) (string, bool) {
	if svc == nil {
		return "", false
	}
	v := svc.List(ctx)
	if !v.Usable() {
		return "", false
	}
	have, pulling := 0, false
	for _, m := range v.Models {
		have += btoi(m.State == ollamapull.StateInstalled || m.State == ollamapull.StateChanged)
		pulling = pulling || m.State == ollamapull.StatePulling
	}
	return fmt.Sprintf("・Ollama のモデル %d / %d 件", have, len(v.Models)), pulling
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ollamaLine is the line of the Ollama of mam in the list of the companion software: where it stands, and how many of its models are fetched. ok is false when this build has no such runtime.
func ollamaLine(ctx context.Context, rts *localruntime.Service, ol *ollamapull.Service) (text, state string, ok bool) {
	v, err := rts.Status(ctx, ollamapull.RuntimeID)
	if err != nil {
		return "", "", false
	}
	switch {
	case !v.Support.Supported:
		return "この PC では使えません（別のサーバーにつなげます）", view.StateEmpty, true
	case v.InstallJob != "":
		return "導入中", view.StateInstalling, true
	case !v.Installed:
		return "未導入", view.StateEmpty, true
	}
	text = "導入済み・" + processWord(v.Process.State)
	if count, pulling := ollamaModels(ctx, ol); count != "" {
		text += count
		if pulling {
			return text + "・取得中", view.StateInstalling, true
		}
	}
	return text, view.StateFull, true
}

// processWord is the state of the process of a server of mam in words.
func processWord(s proc.State) string {
	switch s {
	case proc.StateRunning:
		return "起動中"
	case proc.StateStarting:
		return "起動しています"
	case proc.StateFailed:
		return "起動に失敗"
	}
	return "停止中"
}
