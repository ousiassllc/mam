// Package commercial is how mam says whether a model may be used commercially (#928): the badge, the note that says what to watch for, and the choice of the
// history of the generation test (the lists of models narrow by it in the panel of molecules/modelpick). Every screen that lists or names a model uses
// these, so that the answer looks the same on all of them. The handlers build a View from modellicense.Info; nothing here calls a service. It is a package of
// its own so that molecules stays inside its line limit.
package commercial

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
)

// View is what a screen shows of the license of one model.
type View struct {
	Level       modellicense.Commercial
	LicenseName string
	Condition   string
	SourceURL   string
	CheckedAt   string
	Notice      string
}

// FromInfo is the View of a model's license.
func FromInfo(i modellicense.Info) View {
	return View{Level: i.Commercial, LicenseName: i.LicenseName, Condition: i.Condition, SourceURL: i.SourceURL, CheckedAt: i.CheckedAt, Notice: i.Notice}
}

// Of is the View of the model with the identifier of the generation test.
func Of(modelID string) View { return FromInfo(modellicense.Of(modelID)) }

// Answer is the answer as an attribute value ("yes", "conditional", "no", "unknown"): what the filter reads (data-commercial).
func (v View) Answer() string {
	if !v.Level.Valid() {
		return string(modellicense.Unknown)
	}
	return string(v.Level)
}

// Detail is the tooltip: the license, then the condition or the notice.
func (v View) Detail() string {
	parts := []string{v.LicenseName}
	switch {
	case v.Condition != "":
		parts = append(parts, v.Condition)
	case v.Level != modellicense.Yes && v.Notice != "":
		parts = append(parts, v.Notice)
	}
	return strings.Join(parts, " ／ ")
}

// NeedsNote reports whether the answer carries something to watch for that a screen with room says in words (everything but 商用OK).
func (v View) NeedsNote() bool { return v.Level != modellicense.Yes }

// OptionLabel is the text of a model in a <select>, where only text fits: the name, then the answer in brackets.
func OptionLabel(name string, v View) string {
	if v.Level == modellicense.Yes {
		return name + "（商用OK）"
	}
	return name + "（" + v.Level.Label() + "）"
}

// ChoiceData is the choice 「すべて / 商用OKのみ / 条件つきまで」 of the history of the generation test, a field of the history's form (the history lists results, not
// models, so it has no model filter panel: modelpick). The server narrows the results by it.
type ChoiceData struct {
	// ID is the id of the group; Field is the name its radio buttons post under, so that the form around the choice carries it across its pages.
	ID, Field string
	// Current is the choice that is checked when the page is drawn (the setting "商用OKのモデルだけを使う" decides it when none was asked for).
	Current modellicense.Filter
}

// ShortNote is the one line a small card says in words: the condition of a conditional answer, or that an answer is unknown ("" for
// the others: the badge says it).
func (v View) ShortNote() string {
	switch v.Level {
	case modellicense.Conditional:
		return v.Condition
	case modellicense.Yes, modellicense.No:
		return ""
	}
	return "ライセンスを確認できていません"
}

// NewChoice is the choice of the field, checked at current ("" for all).
func NewChoice(id, field string, current modellicense.Filter) ChoiceData {
	if _, ok := modellicense.ParseFilter(string(current)); !ok || current == "" {
		current = modellicense.FilterAll
	}
	return ChoiceData{ID: id, Field: field, Current: current}
}
