// Package modelfilter is the one set of conditions every screen that lists models narrows them by (#1011): the model manager's catalog, the generation
// test, the project and Shot generation settings, the LLM assignment, and the MCP tools list_model_catalog and list_generation_models. A Condition
// is what the filter panel of a screen holds; it is written as a query string (a screen's URL and form) and as JSON (an MCP call, a stored
// choice) in the same words, and Match judges one Item (a model reduced to what the conditions look at) against it.
//
// It does not know the catalog, the adapter or the screens (it takes the vocabulary of inputs from catalog/traits and of licenses from
// modellicense, nothing else): they reduce their own model types to Items. The words of the conditions are fixed here and in
// spec/architecture/model-manager/model-filter.md.
package modelfilter

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

// Kind is what a model makes or does. A model has one or more: an editing image model is both image and image_edit.
type Kind string

// The kinds, in the order the screens show them.
const (
	KindImage     Kind = "image"
	KindVideo     Kind = "video"
	KindImageEdit Kind = "image_edit" // an image model that edits a picture (inputs edit or inpaint)
	KindUpscale   Kind = "upscale"
	// KindVideoUpscale and KindVideoInterpolate are the models that make an existing video larger and smoother (Issue #1031).
	KindVideoUpscale     Kind = "video_upscale"
	KindVideoInterpolate Kind = "video_interpolate"
	KindLLM              Kind = "llm"
	KindAudio            Kind = "audio"
)

// Kinds lists the kinds in the order the screens show them.
func Kinds() []Kind {
	return []Kind{KindImage, KindVideo, KindImageEdit, KindUpscale, KindVideoUpscale, KindVideoInterpolate, KindLLM, KindAudio}
}

// Origin is where a model runs.
type Origin string

// The origins.
const (
	OriginLocal Origin = "local" // on this PC (ComfyUI and the local runtimes)
	OriginAPI   Origin = "api"   // at a provider (an API key or a subscription)
)

// Origins lists the origins in the order the screens show them.
func Origins() []Origin { return []Origin{OriginLocal, OriginAPI} }

// Band is a range of parameter counts: Small is up to 5B, Medium is above 5B and below 14B, Large is 14B and above. The edges are written in
// the labels of the screens ("〜5B", "5〜14B", "14B〜"): 5B is Small, 14B is Large.
type Band string

// The bands.
const (
	BandSmall  Band = "small"
	BandMedium Band = "medium"
	BandLarge  Band = "large"
)

// The edges of the bands, in billions of parameters.
const (
	SmallMaxB = 5.0  // a model of this size or less is Small
	LargeMinB = 14.0 // a model of this size or more is Large
)

// Bands lists the bands from the smallest.
func Bands() []Band { return []Band{BandSmall, BandMedium, BandLarge} }

// BandOf is the band of a parameter count in billions; false when the count is not known (0 or less).
func BandOf(billions float64) (Band, bool) {
	switch {
	case billions <= 0:
		return "", false
	case billions <= SmallMaxB:
		return BandSmall, true
	case billions < LargeMinB:
		return BandMedium, true
	}
	return BandLarge, true
}

// Label is the label of the band on the screens.
func (b Band) Label() string {
	switch b {
	case BandSmall:
		return "小（〜5B）"
	case BandMedium:
		return "中（5〜14B）"
	case BandLarge:
		return "大（14B〜）"
	}
	return string(b)
}

// AdultFilter narrows by the content rating: all, only general models, or only adult ones.
type AdultFilter string

// The adult filters ("all" is the empty filter).
const (
	AdultAll         AdultFilter = "all"
	AdultGeneralOnly AdultFilter = "general_only"
	AdultOnly        AdultFilter = "adult_only"
)

// UncensoredFilter narrows by traits.uncensored: all, only censored (the original) models, or only uncensored ones.
type UncensoredFilter string

// The uncensored filters ("all" is the empty filter).
const (
	UncensoredAll  UncensoredFilter = "all"
	CensoredOnly   UncensoredFilter = "censored_only"
	UncensoredOnly UncensoredFilter = "uncensored_only"
)

// InstalledFilter narrows by whether a model is installed. A model that has nothing to install on this PC (a cloud model) counts as installed.
type InstalledFilter string

// The installed filters ("all" is the empty filter).
const (
	InstalledAll     InstalledFilter = "all"
	InstalledOnly    InstalledFilter = "installed_only"
	NotInstalledOnly InstalledFilter = "not_installed_only"
)

// Condition is the state of the filter panel. The zero value narrows nothing. Within one list (kinds, origins, providers, bands, speeds,
// qualities, commercial) a model matches when it has one of the values; the inputs must all be there (a model that takes T2V and a first frame is
// asked for with both); every part that is set must match. A model whose value is not known (no parameter count, no speed guide) does not match a
// condition on that value.
//
// The field names are the JSON keys, the query keys and the MCP argument names.
type Condition struct {
	Kinds      []Kind           `json:"kinds,omitempty" jsonschema:"種類で絞り込む（どれか）: image / video / image_edit（画像編集）/ upscale / llm / audio"`
	Inputs     []traits.Input   `json:"inputs,omitempty" jsonschema:"入力で絞り込む（指定したものをすべて持つモデル）: t2i / i2i / inpaint / edit / t2v / i2v / first_frame / last_frame / reference_whole / reference_face / reference_style / reference_pose / reference_control"`
	Adult      AdultFilter      `json:"adult,omitempty" jsonschema:"成人向け（content_rating）: all（省略と同じ）/ general_only（一般向けだけ）/ adult_only（成人向けだけ）"`
	Uncensored UncensoredFilter `json:"uncensored,omitempty" jsonschema:"非検閲（traits.uncensored。成人向けとは別）: all（省略と同じ）/ censored_only / uncensored_only"`
	Params     []Band           `json:"params,omitempty" jsonschema:"パラメータ数の帯（どれか）: small（5B 以下）/ medium（5B より大きく 14B 未満）/ large（14B 以上）。数が分からないモデルは入らない"`
	VRAMFit    bool             `json:"vram_fit,omitempty" jsonschema:"true なら、VRAM 16GB の GPU で動く（vram.min_gb が 16 以下。vram_fit の enough か limited にあたる）ローカルモデルだけ。VRAM の目安が無いモデル（クラウド・LLM）は入らない"`
	Commercial string           `json:"commercial,omitempty" jsonschema:"商用利用で絞り込む。all / ok（商用OK）/ ok_or_conditional（商用OKと条件つき）。ok / conditional / unknown / ng をカンマで並べるとそれらのどれか（例: ok,unknown）"`
	Installed  InstalledFilter  `json:"installed,omitempty" jsonschema:"導入済み: all（省略と同じ）/ installed_only / not_installed_only（クラウドのモデルは導入済み扱い）"`
	Origins    []Origin         `json:"origins,omitempty" jsonschema:"ローカルか API か（どれか）: local / api"`
	Providers  []string         `json:"providers,omitempty" jsonschema:"提供元（プロバイダー。どれか。例: local, anthropic, openai, google, chatgpt-subscription）"`
	Audio      bool             `json:"audio,omitempty" jsonschema:"true なら、音声つきの動画モデルだけ"`
	Speeds     []traits.Level   `json:"speeds,omitempty" jsonschema:"速度の目安（どれか）: fast / standard / slow。目安が無いモデルは入らない"`
	Qualities  []traits.Level   `json:"qualities,omitempty" jsonschema:"品質の目安（どれか）: basic / standard / high。目安が無いモデルは入らない"`
	// AvailableOnly hides what cannot be chosen now (no API key, not signed in, not installed...). The zero value shows everything with its
	// reason; a screen that offers models to choose from starts at Default (this on) and offers "使えないものも表示" to turn it off (#1013).
	AvailableOnly bool   `json:"available_only,omitempty" jsonschema:"true なら、今は選べないモデル（API キー未登録・未ログイン・未導入など）を除く。省略は選べないものも理由つきで返す"`
	Text          string `json:"q,omitempty" jsonschema:"テキスト検索（モデル名・ID・タグ・提供元。空白で区切った語をすべて含む。大文字小文字は区別しない）"`
}

// Default is where a screen that offers models to choose from starts: what cannot be chosen now is hidden (#1013).
func Default() Condition { return Condition{AvailableOnly: true} }

// The words of the commercial condition. They are the words of the existing commercial filter (modellicense.Filter: all, ok, ok_or_conditional)
// and, for a combination that filter cannot say, a comma-separated list of the answers.
const (
	commercialOK          = "ok"
	commercialConditional = "conditional"
	commercialUnknown     = "unknown"
	commercialNG          = "ng"
)

// commercialOrder is the order the answers are written in.
var commercialOrder = []string{commercialOK, commercialConditional, commercialUnknown, commercialNG}

// commercialWords maps a word of the condition to the answer of the license.
var commercialWords = map[string]modellicense.Commercial{
	commercialOK: modellicense.Yes, commercialConditional: modellicense.Conditional, commercialUnknown: modellicense.Unknown, commercialNG: modellicense.No,
}

// CommercialSet is the license answers the condition lets through; nil means every answer (no condition).
func (c Condition) CommercialSet() []modellicense.Commercial {
	set, _ := parseCommercial(c.Commercial)
	return set
}

// parseCommercial reads the commercial word: "" and "all" give nil (everything), "ok" and "ok_or_conditional" the filters of modellicense, anything
// else a comma-separated list of ok / conditional / unknown / ng. The second result is false for a word that is none of them.
func parseCommercial(s string) ([]modellicense.Commercial, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", string(modellicense.FilterAll):
		return nil, true
	case string(modellicense.FilterOK):
		return []modellicense.Commercial{modellicense.Yes}, true
	case string(modellicense.FilterOKOrConditional):
		return []modellicense.Commercial{modellicense.Yes, modellicense.Conditional}, true
	}
	var set []modellicense.Commercial
	for _, w := range strings.Split(s, ",") {
		c, ok := commercialWords[strings.TrimSpace(w)]
		if !ok {
			return nil, false
		}
		if !slices.Contains(set, c) {
			set = append(set, c)
		}
	}
	return set, true
}

// commercialWord is the canonical word for a set of answers: the words of modellicense.Filter where one says it, else the list; "" for none given (Normalize
// turns an explicit "all" and all four answers into "all").
func commercialWord(set []modellicense.Commercial) string {
	has := func(c modellicense.Commercial) bool { return slices.Contains(set, c) }
	switch {
	case len(set) == 0 || len(set) == len(commercialOrder):
		return ""
	case len(set) == 1 && has(modellicense.Yes):
		return string(modellicense.FilterOK)
	case len(set) == 2 && has(modellicense.Yes) && has(modellicense.Conditional):
		return string(modellicense.FilterOKOrConditional)
	}
	var words []string
	for _, w := range commercialOrder {
		if has(commercialWords[w]) {
			words = append(words, w)
		}
	}
	return strings.Join(words, ",")
}

// Validate reports the first value that is none of the words of its condition.
func (c Condition) Validate() error {
	for _, k := range c.Kinds {
		if !slices.Contains(Kinds(), k) {
			return fmt.Errorf("kinds: %q is not a kind (image, video, image_edit, upscale, llm, audio)", k)
		}
	}
	for _, i := range c.Inputs {
		if !i.Valid() {
			return fmt.Errorf("inputs: %q is not an input (%s)", i, joinInputs())
		}
	}
	if !slices.Contains([]AdultFilter{"", AdultAll, AdultGeneralOnly, AdultOnly}, c.Adult) {
		return fmt.Errorf("adult: %q is not all, general_only or adult_only", c.Adult)
	}
	if !slices.Contains([]UncensoredFilter{"", UncensoredAll, CensoredOnly, UncensoredOnly}, c.Uncensored) {
		return fmt.Errorf("uncensored: %q is not all, censored_only or uncensored_only", c.Uncensored)
	}
	for _, b := range c.Params {
		if !slices.Contains(Bands(), b) {
			return fmt.Errorf("params: %q is not small, medium or large", b)
		}
	}
	if _, ok := parseCommercial(c.Commercial); !ok {
		return fmt.Errorf("commercial: %q is not all, ok, ok_or_conditional or a list of ok, conditional, unknown, ng", c.Commercial)
	}
	if !slices.Contains([]InstalledFilter{"", InstalledAll, InstalledOnly, NotInstalledOnly}, c.Installed) {
		return fmt.Errorf("installed: %q is not all, installed_only or not_installed_only", c.Installed)
	}
	for _, o := range c.Origins {
		if !slices.Contains(Origins(), o) {
			return fmt.Errorf("origins: %q is not local or api", o)
		}
	}
	for _, l := range c.Speeds {
		if l == "" || !l.Valid(true) {
			return fmt.Errorf("speeds: %q is not fast, standard or slow", l)
		}
	}
	for _, l := range c.Qualities {
		if l == "" || !l.Valid(false) {
			return fmt.Errorf("qualities: %q is not basic, standard or high", l)
		}
	}
	return nil
}

func joinInputs() string {
	var s []string
	for _, i := range traits.All() {
		s = append(s, string(i))
	}
	return strings.Join(s, ", ")
}
