package llmlocal

import (
	"context"
	"net/url"
	"strings"
)

// Use is the one server the calls of the provider "local" go to (spec/architecture/local-llm.md §11): llama.cpp of mam, Ollama of mam, or the server at the address.
type Use string

// The uses: the values of the form field "use" and of llm.local_server.use of the MCP.
const (
	UseLlamaCpp Use = "llama-cpp"
	UseOllama   Use = "ollama"
	UseCustom   Use = "custom"
)

// Uses are every use in the order of the choice.
func Uses() []Use { return []Use{UseLlamaCpp, UseOllama, UseCustom} }

// ParseUse reads a value of the form field "use" (ok false for a word that is none of the three).
func ParseUse(s string) (Use, bool) {
	for _, u := range Uses() {
		if string(u) == s {
			return u, true
		}
	}
	return "", false
}

// UseOf is the use the two switches of providers.local make (both false: the address).
func UseOf(managedRuntime, managedOllama bool) Use {
	switch {
	case managedRuntime:
		return UseLlamaCpp
	case managedOllama:
		return UseOllama
	}
	return UseCustom
}

// Name is the use as the screens call it.
func (u Use) Name() string {
	switch u {
	case UseLlamaCpp:
		return "この PC の llama.cpp"
	case UseOllama:
		return "Ollama"
	}
	return "別のサーバー"
}

// Managed says that mam installs and starts the server of the use.
func (u Use) Managed() bool { return u != UseCustom }

// on is whether the switch is set to true.
func on(b *bool) bool { return b != nil && *b }

// exclusive is the two switches of a save with the rule that the calls go to one server: choosing one clears the other; both true is refused.
func exclusive(runtime, ollama *bool) (*bool, *bool, error) {
	off := false
	switch {
	case on(runtime) && on(ollama):
		return nil, nil, &InvalidError{Message: "「この PC の llama.cpp」と「Ollama」は同時に使えません。呼び出しの行き先は 1 つです", Field: "use"}
	case on(runtime) && ollama == nil:
		ollama = &off
	case on(ollama) && runtime == nil:
		runtime = &off
	}
	return runtime, ollama, nil
}

// SetUse chooses the server the calls go to: the switches use_managed_runtime / use_managed_ollama of llm.json become the ones of the use (the address
// and the key are kept). Nothing is started. A word that is none of the three is an *InvalidError; a use the app cannot offer is
// ErrManagedUnavailable / ErrManagedOllamaUnavailable; a pinned file is ErrPinned.
func (s *Service) SetUse(u Use) error {
	if _, ok := ParseUse(string(u)); !ok {
		return &InvalidError{Message: "使うサーバーは「この PC の llama.cpp」「Ollama」「別のサーバー」のどれかです", Field: "use"}
	}
	runtime, ollama := u == UseLlamaCpp, u == UseOllama
	return s.Save(SaveInput{UseManaged: &runtime, UseManagedOllama: &ollama})
}

// Status is where the calls of the provider "local" go now, for the section "使うサーバー", the overview of the model manager, the card of the settings and the
// MCP: one description of the use, so that they all say the same (spec/architecture/local-llm.md §11).
type Status struct {
	Use Use
	// Info is where the managed server stands (zero for the use Custom). Unavailable says the use is a managed server that the app does not have.
	Info        ManagedInfo
	Unavailable bool
	// URL is the address the calls go to when the use is Custom: the one the settings keep (it holds no user name, query or key).
	URL string
	// Default says the address is the default one of the settings: the user has not chosen a server.
	Default bool
}

// Status reads where the calls go now. It starts nothing.
func (s *Service) Status(ctx context.Context) Status { return s.StatusOf(ctx, s.settingsView().Use) }

// StatusOf reads where the server of the use stands (the address of the settings for Custom). It starts nothing.
func (s *Service) StatusOf(ctx context.Context, u Use) Status {
	v := s.settingsView()
	st := Status{Use: u, URL: shown(v.BaseURL), Default: v.BaseURL == v.DefaultBaseURL}
	if !u.Managed() {
		return st
	}
	if m := s.managedFor(u); m != nil {
		st.Info = m.Info(ctx)
	} else {
		st.Unavailable = true
	}
	return st
}

// shown is an address as the screens show it: without a user name, a query or a fragment (the settings refuse them, so this only guards a file written by hand).
func shown(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

// Line is the sentence of the state: "今は <名前> に呼び出します".
func (st Status) Line() string { return "今は " + st.Use.Name() + " に呼び出します" }

// Problem is why the use cannot be used now, in words with what to do ("" when it can, or when it is the address: that one is tried by the connection test).
func (st Status) Problem() string {
	switch {
	case !st.Use.Managed():
		return ""
	case st.Unavailable:
		return "このアプリには、mam が管理する " + st.Use.Name() + " がありません。「別のサーバー」を選んでください"
	}
	switch st.Info.State {
	case ManagedUnsupported:
		return "この PC では使えません" + reasonSuffix(st.Info.Reason)
	case ManagedNotInstalled:
		if st.Info.Installing {
			return ""
		}
		return "まだ使えません" + reasonSuffix(st.Info.Reason)
	case ManagedFailed:
		return "起動に失敗しました" + reasonSuffix(st.Info.Reason)
	}
	return ""
}

// Busy says the server installs or starts: the line of its state is read again until that is over.
func (st Status) Busy() bool {
	return st.Use.Managed() && (st.Info.Installing || st.Info.State == ManagedStarting)
}

// Word is the state in two or three words: "導入済み・停止中", "起動中", "未導入" ("" for the address).
func (st Status) Word() string {
	if !st.Use.Managed() {
		return ""
	}
	if st.Unavailable {
		return "このアプリには無い"
	}
	if st.Info.Installing {
		return "導入中"
	}
	switch st.Info.State {
	case ManagedRunning:
		return "起動中"
	case ManagedStarting:
		return "起動しています"
	case ManagedStopped:
		return "導入済み・停止中"
	case ManagedFailed:
		return "起動に失敗"
	case ManagedUnsupported:
		return "この PC では使えません"
	}
	return "未導入"
}

func reasonSuffix(reason string) string {
	if reason = strings.TrimSpace(reason); reason == "" {
		return ""
	}
	return "。" + reason
}
