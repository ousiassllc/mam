// Package modelcatalog lists the models the generation test can use, with what each can do, what it costs and why one cannot be
// chosen now.
package modelcatalog

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/direct"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// Where a model comes from (ModelInfo.Source).
const (
	SourceBuiltin  = "builtin"  // a cloud model with a built-in adapter
	SourceChatGPT  = "chatgpt"  // the image model made with a ChatGPT subscription (#832)
	SourceLocal    = "local"    // a model defined on a registered ComfyUI / OpenAI-compatible endpoint (#831, #833)
	SourceWorkflow = "workflow" // a saved ComfyUI workflow (#834)
)

// How a cost is known (ModelInfo.Price.Basis, Estimate lines).
const (
	BasisPriced  = gentestrepo.CostPriced  // the price is known: USD
	BasisFree    = gentestrepo.CostFree    // a local model: 0 USD
	BasisUnknown = gentestrepo.CostUnknown // no price (a subscription, a cloud model without a price in models.json)
)

// Price is the unit price of a model.
type Price struct {
	Basis string `json:"basis" jsonschema:"priced（単価あり）/ free（ローカル: 0）/ unknown（単価なし。見積りの合計に入れない）"`
	// PerImageUSD is the price of one image (an image model with Basis priced).
	PerImageUSD *float64 `json:"per_image_usd,omitempty"`
	// PerSecondUSD is the price of one second of video by resolution (a video model with Basis priced).
	PerSecondUSD map[string]float64 `json:"per_second_usd,omitempty"`
}

// VideoCaps are the capabilities of a video model.
type VideoCaps struct {
	Durations          []float64 `json:"durations"`
	Resolutions        []string  `json:"resolutions"`
	MaxReferenceImages int       `json:"max_reference_images"`
	FirstFrame         bool      `json:"first_frame"`
	LastFrame          bool      `json:"last_frame"`
	NativeAudio        bool      `json:"native_audio"`
}

// Info is a model the screen can choose, with what it can do and why it cannot be chosen now.
type Info struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Media  string `json:"media"`
	Source string `json:"source"`
	// Available is false when the model cannot be called now; Unavailable is the reason code (domain.ExcludedNoBackend,
	// domain.ExcludedCircuitOpen) and UnavailableMessage the reason for the user.
	Available          bool   `json:"available"`
	Unavailable        string `json:"unavailable,omitempty"`
	UnavailableMessage string `json:"unavailable_message,omitempty"`
	// AcceptsReferences: an image model uses reference images.
	AcceptsReferences bool       `json:"accepts_references"`
	Video             *VideoCaps `json:"video,omitempty"`
	// SupportsSeed: the seed is sent to the model. Without it the n results of a combination are n calls with the same input.
	SupportsSeed bool `json:"supports_seed"`
	// NegativeField: the model has a negative prompt field. Without it the negative prompt is written into the prompt
	// as an "Avoid: ..." sentence.
	NegativeField bool  `json:"negative_field"`
	Price         Price `json:"price"`
}

// Catalog lists the models of the generation test.
type Catalog struct {
	Adapter *modeladapter.Adapter
	// Hidden says a model is left out of the lists and cannot be chosen (the adult models of the model manager while the age
	// gate is closed). Nil hides nothing.
	Hidden func(id string) bool
}

// Models lists the models of the media ("" for both): every built-in cloud model, the models of the registered endpoints,
// the saved ComfyUI workflows and the ChatGPT subscription's image model while it is on. A model that cannot be called
// now is listed with the reason.
func (c Catalog) Models(media string) []Info {
	cfg := c.Adapter.Config()
	out := []Info{}
	if media != gentestrepo.MediaVideo {
		for _, m := range domain.ImageModels() {
			if !c.hidden(m) {
				out = append(out, c.info(cfg, m, gentestrepo.MediaImage))
			}
		}
	}
	if media != gentestrepo.MediaImage {
		for _, m := range domain.VideoModels() {
			if !c.hidden(m) {
				out = append(out, c.info(cfg, m, gentestrepo.MediaVideo))
			}
		}
	}
	return out
}

func (c Catalog) hidden(m domain.ModelName) bool { return c.Hidden != nil && c.Hidden(string(m)) }

// Model returns one model of the media; false when there is none.
func (c Catalog) Model(media, id string) (Info, bool) {
	for _, m := range c.Models(media) {
		if m.ID == id {
			return m, true
		}
	}
	return Info{}, false
}

func (c Catalog) info(cfg modeladapter.Config, m domain.ModelName, media string) Info {
	mi := Info{
		ID: string(m), Label: modelreg.Label(string(m)), Media: media, Source: sourceOf(cfg, m),
		SupportsSeed: direct.SupportsSeed(cfg, m), NegativeField: direct.NegativeField(cfg, m), Price: priceOf(cfg, m, media),
	}
	if reason := c.Adapter.Unavailable(m); reason != "" {
		mi.Unavailable, mi.UnavailableMessage = reason, unavailableMessage(reason, mi.Source)
	} else {
		mi.Available = true
	}
	if media == gentestrepo.MediaImage {
		mi.AcceptsReferences = cfg.AcceptsImages(m)
		return mi
	}
	if caps, ok := cfg.VideoCaps(m); ok {
		mi.Video = &VideoCaps{
			Durations: slices.Clone(caps.Durations), Resolutions: slices.Clone(caps.Resolutions), MaxReferenceImages: caps.MaxReferenceImages,
			FirstFrame: caps.FirstFrame, LastFrame: caps.LastFrame, NativeAudio: caps.NativeAudio,
		}
	}
	return mi
}

func sourceOf(cfg modeladapter.Config, m domain.ModelName) string {
	if _, ok := cfg.Workflows[string(m)]; ok {
		return SourceWorkflow
	}
	if m == chatgptid.Model {
		return SourceChatGPT
	}
	if _, _, ok := cfg.LocalModel(m); ok {
		return SourceLocal
	}
	return SourceBuiltin
}

// priceOf is the unit price: a local model is free unless the user priced it, the ChatGPT subscription's model has no price
// (the plan pays), and a cloud model without a price in models.json is unknown.
func priceOf(cfg modeladapter.Config, m domain.ModelName, media string) Price {
	if m == chatgptid.Model {
		return Price{Basis: BasisUnknown}
	}
	lm, _, local := cfg.LocalModel(m)
	if media == gentestrepo.MediaImage {
		c := cfg.ImageCost(m)
		switch {
		case c > 0:
			return Price{Basis: BasisPriced, PerImageUSD: &c}
		case local:
			return Price{Basis: BasisFree}
		}
		return Price{Basis: BasisUnknown}
	}
	caps, _ := cfg.VideoCaps(m)
	prices := map[string]float64{}
	for _, res := range caps.Resolutions {
		if p := cfg.VideoCost(modeladapter.VideoTarget{Model: m, Resolution: res}, 1); p > 0 {
			prices[res] = p
		}
	}
	switch {
	case len(prices) > 0:
		return Price{Basis: BasisPriced, PerSecondUSD: prices}
	case local && lm.CostPerSecond == 0:
		return Price{Basis: BasisFree}
	}
	return Price{Basis: BasisUnknown}
}

func unavailableMessage(reason, source string) string {
	switch {
	case reason == agegate.Excluded:
		return "年齢確認がオフのため、成人向けモデルは使えません（モデル管理で年齢確認をオンにしてください）"
	case reason == domain.ExcludedCircuitOpen:
		return "このモデルのベンダーで失敗が続いたため、しばらく止めています。少し待ってからもう一度お試しください"
	case source == SourceChatGPT:
		return "ChatGPT にログインしていないか、画像生成が OFF です（設定の「ChatGPT でログイン」）"
	case source == SourceLocal || source == SourceWorkflow:
		return "接続先を使えません（設定の「ローカル・代替モデル」を確認してください）"
	}
	return "API キーが登録されていません（設定の「API キー」）"
}
