package localllm

import (
	"context"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/llmcard"
)

// noManaged is why the servers of mam are not offered: the app has none.
const noManaged = "このアプリには、mam が管理するサーバー（llama.cpp・Ollama）がないため、選べるのは「別のサーバー」だけです。下の URL に、起動しているサーバーを指定してください。"

// aboutOf is the line under the name of a server in the radio group.
var aboutOf = map[llmlocal.Use]string{
	llmlocal.UseLlamaCpp: "mam が導入・起動する llama.cpp です。下の「ローカルで動かすサーバー」で導入し、使うモデルを選びます。最初の LLM の呼び出しで起動します。",
	llmlocal.UseOllama:   "mam が導入・起動する Ollama です。下の「ローカルで動かすサーバー」で導入します。最初の LLM の呼び出しで起動します。",
	llmlocal.UseCustom:   "LM Studio・自分で入れた Ollama・llama-server など。下の URL に呼び出します。",
}

// UseData is the section "使うサーバー" as saved now. Nothing is started.
func UseData(ctx context.Context, svc *llmlocal.Service) llmcard.Use {
	v := svc.View()
	u := llmcard.Use{Selected: string(v.Use), Pinned: v.Pinned, Problem: v.Problem, State: stateOf(svc.Status(ctx))}
	for _, use := range llmlocal.Uses() {
		if !svc.Offers(use) {
			continue
		}
		c := llmcard.Choice{Use: string(use), Name: use.Name(), About: aboutOf[use]}
		if use.Managed() {
			c.State = svc.StatusOf(ctx, use).Word()
		}
		u.Choices = append(u.Choices, c)
	}
	if !svc.Offers(llmlocal.UseLlamaCpp) && !svc.Offers(llmlocal.UseOllama) {
		u.NoManaged = noManaged
	}
	return u
}

// stateOf is the line of the state of the server in use. The data-state is attention for a server that cannot be used, installing while one installs, empty while
// nothing was chosen (the address is the default and no server of mam is in use), else ready.
func stateOf(st llmlocal.Status) llmcard.State {
	s := llmcard.State{Line: st.Line(), Problem: st.Problem(), Kind: "ready", Polling: st.Busy()}
	switch {
	case st.Use.Managed() && st.Info.Installing:
		s.Kind = "installing"
	case s.Problem != "":
		s.Kind = "attention"
	case !st.Use.Managed() && st.Default:
		s.Kind = "empty"
	}
	return s
}

// StateOOB is the line of the state drawn out of band: the answer of a card of a server of mam carries it, because an install, a start or a removal changes it.
func StateOOB(ctx context.Context, svc *llmlocal.Service) templ.Component {
	return llmcard.StateOOB(stateOf(svc.Status(ctx)))
}

// ConnectionData is the section "別のサーバーにつなぐ" as saved now.
func ConnectionData(svc *llmlocal.Service) llmcard.Connection {
	v := svc.View()
	return llmcard.Connection{
		BaseURL: v.BaseURL, DefaultBaseURL: v.DefaultBaseURL, KeyConfigured: v.KeyConfigured, KeyHint: v.KeyHint, KeyUnreadable: v.KeyUnreadable,
		Unload: v.UnloadBeforeGeneration, Note: v.Use != llmlocal.UseCustom, Pinned: v.Pinned, Problem: v.Problem,
	}
}

func resultOf(r llmlocal.TestResult) llmcard.TestResult {
	out := llmcard.TestResult{OK: r.OK, Message: r.Message, Models: r.Models, Count: r.Count}
	if r.Error != nil {
		v := r.Error.View(apperr.LangJa)
		out.Failure = &v
	}
	return out
}
