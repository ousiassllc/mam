package modelpick

import (
	"net/url"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// Chip is a filter in force, as the chip above the list: its words and the address that draws the list again without it.
type Chip struct {
	Label  string
	Remove string
}

// KindLabel is the label of a kind.
func KindLabel(k modelfilter.Kind) string {
	switch k {
	case modelfilter.KindImage:
		return "画像"
	case modelfilter.KindVideo:
		return "動画"
	case modelfilter.KindImageEdit:
		return "画像編集"
	case modelfilter.KindUpscale:
		return "アップスケール"
	case modelfilter.KindVideoUpscale:
		return "動画アップスケール"
	case modelfilter.KindVideoInterpolate:
		return "コマ補間"
	case modelfilter.KindLLM:
		return "LLM"
	case modelfilter.KindAudio:
		return "音声"
	}
	return string(k)
}

// OriginLabel is the label of where a model runs.
func OriginLabel(o modelfilter.Origin) string {
	if o == modelfilter.OriginLocal {
		return "ローカル"
	}
	return "クラウド"
}

// AdultLabel is the label of a choice of the content-rating filter (UncensoredLabel and InstalledLabel are those of the other three-way filters).
func AdultLabel(a modelfilter.AdultFilter) string {
	switch a {
	case modelfilter.AdultGeneralOnly:
		return "一般向けのみ"
	case modelfilter.AdultOnly:
		return "成人向けのみ"
	}
	return "すべて"
}

// UncensoredLabel is the label of the choice of the uncensored filter.
func UncensoredLabel(u modelfilter.UncensoredFilter) string {
	switch u {
	case modelfilter.CensoredOnly:
		return "通常のみ"
	case modelfilter.UncensoredOnly:
		return "非検閲のみ"
	}
	return "すべて"
}

// InstalledLabel is the label of the choice of the installed filter.
func InstalledLabel(i modelfilter.InstalledFilter) string {
	switch i {
	case modelfilter.InstalledOnly:
		return "導入済みのみ"
	case modelfilter.NotInstalledOnly:
		return "未導入のみ"
	}
	return "すべて"
}

func commercialLabel(c modelfilter.Condition) string {
	if f, ok := modellicense.ParseFilter(c.Commercial); ok {
		return f.Label()
	}
	var names []string
	for _, a := range c.CommercialSet() {
		names = append(names, a.Label())
	}
	return strings.Join(names, "・")
}

// providerLabel is the label of a provider: the one the screen gives it, else its word.
func (b Filter) providerLabel(v string) string {
	for _, o := range b.Facets.Providers {
		if o.Value == v {
			return o.Label
		}
	}
	return v
}

// URL is the address that draws the results for the condition c: the bar's action with the condition's words and the markers that say the request states the
// condition (so that the remembered one is not read) and asks for the panel to be drawn again (the request came from a chip or a button, not from the panel,
// so the panel does not show it yet).
func (b Filter) URL(c modelfilter.Condition) string {
	u, err := url.Parse(b.Action)
	if err != nil {
		return b.Action
	}
	if c.Commercial == string(b.DefaultCommercial) {
		c.Commercial = "" // the setting's own answer is not stated: the screen goes on following the setting
	}
	q := u.Query()
	for _, k := range modelfilter.Keys() {
		q.Del(k)
	}
	for k, v := range c.Query(b.Start) {
		q[k] = v
	}
	q.Set(KeyMarker, "1")
	q.Set(KeySync, "1")
	u.RawQuery = q.Encode()
	return u.String()
}

// ClearAll is the condition of "すべてクリア": the screen's starting point with the commercial part set to all too (a chip of the setting 「商用OKのモデルだけを使う」 is a
// filter in force, and clearing means that nothing narrows the list). A screen that starts at "選べるものだけ" stays there: it has its own switch.
func (b Filter) ClearAll() modelfilter.Condition {
	c := b.Start
	c.Commercial = string(modellicense.FilterAll)
	return c.Normalize()
}

// without is c with the part dropped by edit (on a copy: the lists are cloned first).
func without(c modelfilter.Condition, edit func(*modelfilter.Condition)) modelfilter.Condition {
	c.Kinds, c.Inputs, c.Params = slices.Clone(c.Kinds), slices.Clone(c.Inputs), slices.Clone(c.Params)
	c.Origins, c.Providers, c.Speeds, c.Qualities = slices.Clone(c.Origins), slices.Clone(c.Providers), slices.Clone(c.Speeds), slices.Clone(c.Qualities)
	edit(&c)
	return c
}

func dropValue[T comparable](list []T, v T) []T {
	return slices.DeleteFunc(slices.Clone(list), func(x T) bool { return x == v })
}

// Chips are the filters in force, one chip each (a list of several values gives a chip per value), in the order of the panel. A chip's Remove is the address that
// draws the list without it. The switch "選べるものだけ" is a chip too when it is not at the screen's starting point.
func (b Filter) Chips() []Chip {
	c := b.Cond
	var out []Chip
	add := func(label string, edit func(*modelfilter.Condition)) {
		out = append(out, Chip{Label: label, Remove: b.URL(without(c, edit))})
	}
	for _, k := range c.Kinds {
		add("種類: "+KindLabel(k), func(x *modelfilter.Condition) { x.Kinds = dropValue(x.Kinds, k) })
	}
	for _, in := range c.Inputs {
		add("入力: "+in.Label(), func(x *modelfilter.Condition) { x.Inputs = dropValue(x.Inputs, in) })
	}
	if c.Adult != "" && c.Adult != modelfilter.AdultAll {
		add("成人向け: "+AdultLabel(c.Adult), func(x *modelfilter.Condition) { x.Adult = "" })
	}
	if c.Uncensored != "" && c.Uncensored != modelfilter.UncensoredAll {
		add("非検閲: "+UncensoredLabel(c.Uncensored), func(x *modelfilter.Condition) { x.Uncensored = "" })
	}
	for _, p := range c.Params {
		add("パラメータ数: "+p.Label(), func(x *modelfilter.Condition) { x.Params = dropValue(x.Params, p) })
	}
	if c.VRAMFit {
		add("16GB で動く", func(x *modelfilter.Condition) { x.VRAMFit = false })
	}
	if len(c.CommercialSet()) > 0 {
		add("商用利用: "+commercialLabel(c), func(x *modelfilter.Condition) { x.Commercial = string(modellicense.FilterAll) })
	}
	if c.Installed != "" && c.Installed != modelfilter.InstalledAll {
		add(InstalledLabel(c.Installed), func(x *modelfilter.Condition) { x.Installed = "" })
	}
	for _, o := range c.Origins {
		add("提供元: "+OriginLabel(o), func(x *modelfilter.Condition) { x.Origins = dropValue(x.Origins, o) })
	}
	for _, p := range c.Providers {
		add("提供元: "+b.providerLabel(p), func(x *modelfilter.Condition) { x.Providers = dropValue(x.Providers, p) })
	}
	if c.Audio {
		add("音声つき", func(x *modelfilter.Condition) { x.Audio = false })
	}
	for _, l := range c.Speeds {
		add("速度: "+l.Label(), func(x *modelfilter.Condition) { x.Speeds = dropValue(x.Speeds, l) })
	}
	for _, l := range c.Qualities {
		add("品質: "+l.Label(), func(x *modelfilter.Condition) { x.Qualities = dropValue(x.Qualities, l) })
	}
	if c.Text != "" {
		add("検索: 「"+c.Text+"」", func(x *modelfilter.Condition) { x.Text = "" })
	}
	if c.AvailableOnly != b.Start.AvailableOnly {
		label := "使えないものも表示"
		if c.AvailableOnly {
			label = "選べるものだけ"
		}
		add(label, func(x *modelfilter.Condition) { x.AvailableOnly = b.Start.AvailableOnly })
	}
	return out
}

// ShowHidden is the address that draws the list with the models that cannot be chosen now too: what the note 「N 件は今は選べません」 offers.
func (b Filter) ShowHidden() string {
	return b.URL(without(b.Cond, func(x *modelfilter.Condition) { x.AvailableOnly = false }))
}

// inputLabel is the label of an input in the panel.
func inputLabel(in traits.Input) string { return in.Label() }
