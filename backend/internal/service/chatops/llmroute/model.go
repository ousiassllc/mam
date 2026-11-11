package llmroute

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
	"github.com/ousiassllc/mam/backend/internal/service/llmassign"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
)

// model is the model of the new target: its ID, and the list of its provider it was looked up in (nil when the provider's models could not be listed).
type model struct {
	id   string
	list *llmmodels.List
}

// modelFor resolves the model the operation names against the list of the provider. A name the list does not have is taken as it is (the card warns, as the MCP tool does) when
// it can be a model name; one with blanks in it cannot, and is refused with the candidates.
func (h *Handler) modelFor(ctx context.Context, provider string, a Args) (model, error) {
	text := strings.TrimSpace(a.Model)
	if h.deps.List == nil {
		return model{id: text}, checkName(text, nil)
	}
	l, err := h.deps.List(ctx, provider)
	if err != nil {
		if errors.Is(err, llmmodels.ErrUnknownProvider) {
			return model{}, chatops.Refuse("プロバイダー %s は選べません", provider)
		}
		return model{id: text}, checkName(text, nil)
	}
	switch found := pickModels(l, text); {
	case len(found) == 1:
		return model{id: found[0].ID, list: &l}, nil
	case len(found) > 1 && len(found) <= chatops.MaxChoices:
		amb := &ambiguity{text: text}
		for _, f := range found {
			pick := a
			pick.Provider, pick.Model = provider, f.ID
			amb.choices, amb.labels = append(amb.choices, pick), append(amb.labels, modelLabel(f))
		}
		return model{}, amb
	case len(found) > 1:
		return model{}, chatops.Refuse("「%s」に当てはまるモデルが多すぎます（%s）。もう少し具体的に言ってください", text, nlu.Few(modelLabels(found), 8))
	}
	return model{id: text, list: &l}, checkName(text, l.Models)
}

func modelLabels(ms []llmmodels.Model) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, modelLabel(m))
	}
	return out
}

// checkName refuses a name that cannot be a model name (blanks in it, too long) before a card asks about it; the candidates, when the provider has a list, say what to say instead.
func checkName(text string, candidates []llmmodels.Model) error {
	switch {
	case strings.IndexFunc(text, func(r rune) bool { return r > unicode.MaxASCII || unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		msg := "「" + text + "」はモデル名として読み取れません（モデル名は、空白を含まない半角の英数字と - _ . : / などで書きます）。"
		if len(candidates) > 0 {
			msg += "選べるモデル: " + nlu.Few(modelLabels(candidates), 8)
		}
		return &chatops.Refusal{Message: msg}
	case len(text) > llmassign.MaxModelBytes:
		return chatops.Refuse("モデル名が長すぎます")
	}
	return nil
}

// warningsFor are the facts the director should know before saying yes: the provider cannot be used now (no API key, not signed in), the model is in no list of the provider.
// None of them refuses: the assignment is saved all the same, as in the MCP tool.
func (h *Handler) warningsFor(provider string, m model) []string {
	var out []string
	if h.deps.Availability != nil {
		if a, err := h.deps.Availability(provider); err == nil && !a.Available {
			out = append(out, provider+" は今使えません: "+a.Message+"（保存はできますが、設定が済むまでこの Agent の呼び出しは失敗します）")
		}
	}
	if m.list != nil && !m.list.Has(m.id) {
		out = append(out, unlisted(m.id, *m.list))
	}
	return out
}

// unlisted says that a model is in no list of its provider.
func unlisted(model string, l llmmodels.List) string {
	switch {
	case l.Fallback == llmmodels.FallbackLocalDown:
		return "「" + model + "」を確かめられません（" + l.Fallback.Message() + "）。保存はできます。サーバーを起動してから選べる名前を確かめてください"
	case l.Source == llmmodels.SourceAPI:
		return "「" + model + "」は " + l.Provider + " の一覧にありません。使えないかもしれません（保存はできます）"
	}
	return "「" + model + "」は mam の既定候補にありません（" + l.Provider + ": " + l.Fallback.Message() + "）。新しいモデルなら使えることもありますが、使えないかもしれません（保存はできます）"
}
