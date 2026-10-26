// Package chatgptcodex is the Reasoning LLM backend of the provider "chatgpt-codex": the Codex app-server (a child
// process of mam, signed in with a ChatGPT account of any plan, including Business / Enterprise workspaces), called
// through package chatgpt/codex (spec/architecture/chatgpt-signin.md §8). Text generation only.
//
// It maps what a call of the app-server needs of the caller to the classes of llm.APIError: a usage limit, a sign-in to
// repeat or an engine to install is Permanent (not retried, not counted by the circuit breaker, and told to the chat
// with a Notice), a connection or process trouble is retried, anything else is not.
package chatgptcodex

import (
	"context"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
)

const provider = llm.ProviderChatGPTCodex

// Completer is what the backend needs of the Codex login: *codex.Service implements it.
type Completer interface {
	Complete(ctx context.Context, req codex.Request) (string, error)
}

// Backend implements llm.Backend.
type Backend struct{ c Completer }

var _ llm.Backend = (*Backend)(nil)

// New returns the backend.
func New(c Completer) *Backend { return &Backend{c: c} }

// Add registers the backend of llm.ProviderChatGPTCodex in backends when cfg has that provider.
func Add(backends map[llm.Provider]llm.Backend, cfg llm.Config, c Completer) {
	if _, ok := cfg.Providers[provider]; ok {
		backends[provider] = New(c)
	}
}

// Complete implements llm.Backend.
func (b *Backend) Complete(ctx context.Context, model string, req llm.Request) (string, error) {
	r := codex.Request{Model: model, System: req.System, User: req.User, JSON: req.JSON}
	for _, img := range req.Images {
		r.Images = append(r.Images, codex.Image{MimeType: img.MimeType, Data: img.Data, Caption: img.Caption})
	}
	text, err := b.c.Complete(ctx, r)
	if err != nil {
		return "", mapError(err)
	}
	return text, nil
}

// mapError turns a failure of the Codex login into an llm.APIError. The end of the context is returned as it is: the
// Client tells a timeout from a cancel by it.
func mapError(err error) error {
	var f *codex.Failure
	if !errors.As(err, &f) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &llm.APIError{Provider: provider, Err: err}
	}
	e := &llm.APIError{Provider: provider, Message: f.Message, Err: f.Err}
	switch f.Kind {
	case codex.KindUsageLimit:
		e.Status, e.Permanent = http.StatusTooManyRequests, true
		e.Notice = "ChatGPT のプラン（Codex 経由）の利用上限に達しました。" + resetText(f) + "利用状況: " + chatgpt.UsageURL
	case codex.KindNotSignedIn:
		e.Permanent = true
		e.Notice = "ChatGPT に Codex 経由でログインしていません。設定画面の「ChatGPT でログイン」の「Codex 経由」からログインしてください。"
	case codex.KindUnauthorized:
		e.Status, e.Permanent = http.StatusUnauthorized, true
		e.Notice = "ChatGPT（Codex 経由）のログインが無効になっています。設定画面でログインし直してください。"
	case codex.KindEngine:
		e.Permanent = true
		e.Notice = "Codex のエンジンが導入されていません。設定画面の「ChatGPT でログイン」の「Codex 経由」で導入してください。"
	case codex.KindUnsupported:
		e.Status, e.Permanent = http.StatusBadRequest, true
		e.Notice = f.Message
	case codex.KindTransient:
		e.Status = http.StatusServiceUnavailable
	default: // KindPermanent: the request will fail again
		e.Status, e.Permanent = http.StatusUnprocessableEntity, true
	}
	return e
}

func resetText(f *codex.Failure) string {
	if f.ResetsAt.IsZero() {
		return ""
	}
	return "リセットは " + codex.FormatReset(f.ResetsAt) + " です。"
}
