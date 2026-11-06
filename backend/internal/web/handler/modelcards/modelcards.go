// Package modelcards turns the models the services list (the generation test's modelcatalog.Info, the model manager's mmview.Entry, the LLM assignment's
// llmmodels.Candidate, the image and video assignment's options) into the one card every list of models shows (molecules/modelcard, #1011). The handler of a
// screen calls it, so that a model says the same things the same way wherever it is listed: the name, where it comes from and what it costs, the commercial-use
// icon, the badges (成人向け, the parameter count, the VRAM, 導入済み, how it uses input images) and, when it cannot be chosen now, why and where to fix it.
package modelcards

import (
	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// SourceLabel is where a generation-test model comes from, in words.
var SourceLabel = map[string]string{
	modelcatalog.SourceBuiltin:  "クラウド",
	modelcatalog.SourceChatGPT:  "ChatGPT サブスク",
	modelcatalog.SourceLocal:    "ローカル",
	modelcatalog.SourceWorkflow: "保存したワークフロー",
}

// Unavailable is the reason of a model that cannot be chosen now, with the screen that fixes it: nil when the availability says it can.
func Unavailable(a modelfilter.Availability) *modelcard.Reason {
	if a.Available {
		return nil
	}
	return &modelcard.Reason{Text: a.Message, Route: a.Route}
}

// FromInfo is the card of a model of the generation test: an image or a video model, local or cloud. The caller makes it a choice (Pick) or draws its summary.
func FromInfo(m modelcatalog.Info) modelcard.Card {
	icon := atoms.IconImage
	if m.Media == "video" {
		icon = atoms.IconVideo
	}
	answer := commercial.FromInfo(m.License)
	card := modelcard.Card{
		ID: "mp-" + m.ID, Name: m.Label, Icon: icon, Meta: SourceLabel[m.Source] + " ・ " + gtshare.PriceText(m.Price),
		Commercial: &answer, Note: answer.ShortNote(), Reason: Unavailable(m.Availability()),
	}
	if m.Media != "video" {
		card.Badges = append(card.Badges, RefBadge(m.ReferenceMode, m.ReferenceRoles))
	}
	card.Badges = append(card.Badges, FactsOf(m).Badges()...)
	return card
}

// FactsOf is what the card says of a generation-test model besides its name: the content rating, the parameter count and the VRAM guideline (from the model
// manager's catalog, for a model it installed) and that a local model is installed (a model of this list is).
func FactsOf(m modelcatalog.Info) modelcard.Facts {
	f := modelcard.Facts{Installed: m.Origin == string(modelfilter.OriginLocal)}
	if c := m.Catalog; c != nil {
		f.Adult, f.ParamsLabel, f.VRAMMinGB = c.Adult, c.Traits.ParamsLabel, c.VRAMMinGB
	}
	return f
}

// RefBadge says whether an image model uses input images and how: an icon with its tooltip, and for a model that says its mode the short word (編集 = an editing
// model with several images, 画像から = one image the sampling starts from).
func RefBadge(mode string, roles []string) modelcard.Badge {
	b := modelcard.Badge{Tone: atoms.BadgeNeutral, Attrs: templ.Attributes{"data-testid": "gt-model-ref", "data-gt-ref-icon": true}}
	switch {
	case len(roles) == 0:
		b.Icon, b.Label, b.Attrs["title"] = atoms.IconImageOff, "入力画像なし", "入力画像は使えません"
	case mode == "edit":
		b.Icon, b.Label, b.Attrs["title"] = atoms.IconImagePlus, "編集", "入力画像: 編集（複数枚・役割つき）"
	case mode == "img2img":
		b.Icon, b.Label, b.Attrs["title"] = atoms.IconImageUp, "画像から", "入力画像: 画像から（1 枚を元に生成）"
	default:
		b.Icon, b.Label, b.Attrs["title"] = atoms.IconImage, "入力画像あり", "入力画像を使えます"
	}
	return b
}

// Choose makes the card a choice of a model picker: choosing it puts values into the field's hidden inputs (the key "value" is the main one).
func Choose(c modelcard.Card, values map[string]string) modelcard.Card {
	c.Pick = &modelcard.Pick{Mode: modelcard.PickOne, Values: values}
	return c
}

// Facets are the filters the panel offers for the generation-test models of a media: what tells them apart. The providers are those the models come from.
func Facets(media string, models []modelcatalog.Info) modelpick.Facets {
	f := modelpick.Facets{
		Adult: true, Uncensored: true, Params: true, VRAM: true, Commercial: true, Origins: true, Speed: true, Quality: true, Available: true,
		Providers: providers(models),
	}
	if media == "video" {
		f.Inputs = []traits.Input{traits.T2V, traits.I2V, traits.FirstFrame, traits.LastFrame, traits.ReferenceWhole}
		f.Audio = true
		return f
	}
	f.Kinds = []modelfilter.Kind{modelfilter.KindImage, modelfilter.KindImageEdit}
	f.Inputs = []traits.Input{traits.T2I, traits.I2I, traits.Edit, traits.Inpaint, traits.ReferenceWhole, traits.ReferenceFace, traits.ReferenceStyle, traits.ReferencePose}
	return f
}

// providers are the providers the models come from (the local ones are the origin "ローカル"), in the order they first appear, as the choices of the panel.
func providers(models []modelcatalog.Info) []modelpick.Option {
	var out []modelpick.Option
	seen := map[string]bool{}
	for _, m := range models {
		if m.Provider == "" || m.Provider == "local" || seen[m.Provider] {
			continue
		}
		seen[m.Provider] = true
		out = append(out, modelpick.Option{Value: m.Provider, Label: modelpick.ProviderLabel(m.Provider)})
	}
	return out
}

// UpscaleModeText says how an upscale workflow enlarges, in the box's chooser and the detail's "拡大" row ("" for a mode it does not know).
func UpscaleModeText(mode string) string {
	switch mode {
	case modelcatalog.ModeModel:
		return "アップスケール用モデルだけ"
	case modelcatalog.ModeTwoPass:
		return "そのあと低い denoise でもう一度サンプリングして仕上げる。元のプロンプトを引き継ぐ"
	}
	return ""
}

// FromUpscaler is the card of an upscale workflow: the name, how it enlarges, the commercial-use icon and, when it cannot run now, why.
func FromUpscaler(u modelcatalog.Upscaler) modelcard.Card {
	answer := commercial.FromInfo(u.License)
	return modelcard.Card{
		ID: "mp-" + u.ID, Name: u.Label, Icon: atoms.IconImage, Meta: SourceLabel[modelcatalog.SourceLocal] + " ・ " + UpscaleModeText(u.Mode),
		Commercial: &answer, Note: answer.ShortNote(), Reason: Unavailable(u.Availability()),
	}
}

// FromVideoEnhancer is the card of a model that makes a video larger or smoother: the name, what it does and where it runs (the paid cloud says it costs), the
// commercial-use icon and, when it cannot run now, why.
func FromVideoEnhancer(v modelcatalog.VideoEnhancer) modelcard.Card {
	answer := commercial.FromInfo(v.License)
	where, what := SourceLabel[modelcatalog.SourceLocal]+"（無料）", "大きくする"
	if v.Paid {
		where = "Topaz（有料クラウド）"
	}
	if v.Op == "interpolate" {
		what = "滑らかにする（コマを増やす）"
	}
	return modelcard.Card{
		ID: "mp-" + v.ID, Name: v.Label, Icon: atoms.IconVideo, Meta: where + " ・ " + what,
		Commercial: &answer, Note: answer.ShortNote(), Reason: Unavailable(v.Availability()),
	}
}
