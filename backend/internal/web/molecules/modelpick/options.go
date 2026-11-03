package modelpick

import (
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// orWord is the word of a three-way filter: the empty word is the first choice.
func orWord(v, all string) string {
	if v == "" {
		return all
	}
	return v
}

func kindWords(kinds []modelfilter.Kind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = string(k)
	}
	return out
}

func kindOptions(kinds []modelfilter.Kind) []Option {
	out := make([]Option, len(kinds))
	for i, k := range kinds {
		out[i] = Option{string(k), KindLabel(k)}
	}
	return out
}

func inputOptions(inputs []traits.Input) []Option {
	out := make([]Option, len(inputs))
	for i, in := range inputs {
		out[i] = Option{string(in), inputLabel(in)}
	}
	return out
}

func inputWords(inputs []traits.Input) []string {
	out := make([]string, len(inputs))
	for i, in := range inputs {
		out[i] = string(in)
	}
	return out
}

func bandOptions() []Option {
	var out []Option
	for _, b := range modelfilter.Bands() {
		out = append(out, Option{string(b), b.Label()})
	}
	return out
}

func bandWords(bands []modelfilter.Band) []string {
	out := make([]string, len(bands))
	for i, b := range bands {
		out[i] = string(b)
	}
	return out
}

func levelOptions(speed bool) []Option {
	if speed {
		return []Option{{string(traits.Fast), traits.Fast.Label()}, {string(traits.Standard), traits.Standard.Label()}, {string(traits.Slow), traits.Slow.Label()}}
	}
	return []Option{{string(traits.Basic), traits.Basic.Label()}, {string(traits.Standard), traits.Standard.Label()}, {string(traits.High), traits.High.Label()}}
}

func levelWords(levels []traits.Level) []string {
	out := make([]string, len(levels))
	for i, l := range levels {
		out[i] = string(l)
	}
	return out
}

func adultOptions() []Option {
	var out []Option
	for _, a := range []modelfilter.AdultFilter{modelfilter.AdultAll, modelfilter.AdultGeneralOnly, modelfilter.AdultOnly} {
		out = append(out, Option{string(a), AdultLabel(a)})
	}
	return out
}

func uncensoredOptions() []Option {
	var out []Option
	for _, u := range []modelfilter.UncensoredFilter{modelfilter.UncensoredAll, modelfilter.CensoredOnly, modelfilter.UncensoredOnly} {
		out = append(out, Option{string(u), UncensoredLabel(u)})
	}
	return out
}

func installedOptions() []Option {
	var out []Option
	for _, i := range []modelfilter.InstalledFilter{modelfilter.InstalledAll, modelfilter.InstalledOnly, modelfilter.NotInstalledOnly} {
		out = append(out, Option{string(i), InstalledLabel(i)})
	}
	return out
}

// commercialOptions are the three choices of the commercial use (すべて / 商用OKのみ / 条件つきまで). The one that equals what the setting says is sent as the
// empty word, so choosing it leaves the screen following the setting; the others are sent as they are.
func (b Filter) commercialOptions() []Option {
	var out []Option
	for _, f := range modellicense.Filters() {
		value := string(f)
		if f == b.defaultCommercial() {
			value = ""
		}
		out = append(out, Option{value, f.Label()})
	}
	return out
}

func (b Filter) defaultCommercial() modellicense.Filter {
	if b.DefaultCommercial == "" {
		return modellicense.FilterAll
	}
	return b.DefaultCommercial
}

// commercialCurrent is the value of the choice that is in force: the empty word for the setting's, the word of the condition for another one ("" is also
// what a condition that is none of the three choices leaves: nothing is marked, but the chip says what it is).
func (b Filter) commercialCurrent() string {
	word := b.Cond.Commercial
	if word == "" || word == string(b.defaultCommercial()) {
		return ""
	}
	return word
}

// ProviderLabel is the label of a provider (a word of the condition's providers): the vendor's name as it is written.
func ProviderLabel(p string) string {
	switch p {
	case "local":
		return "ローカル"
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "google":
		return "Google"
	case modelfilter.ProviderChatGPT:
		return "ChatGPT サブスク"
	}
	return p
}
