package modelpicker

import (
	"context"
	"net/url"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// notInpaint is why a model that cannot redraw part of a picture is listed but not chosen on the partial redraw's picker.
const notInpaint = "一部だけ描き直すに対応していません"

func init() {
	sources[SourceMatch] = gentestSource("プロンプトマッチのモデル", gentest.MediaImage, nil)
	sources[SourceInpaint] = gentestSource("描き直すモデル", gentest.MediaImage, inpaintOnly)
	sources[SourceShotVideo] = gentestSource("動画モデル", gentest.MediaVideo, nil)
	sources[SourceSettingsImage] = source{
		title: "画像モデル", empty: "条件に合うモデルがありません。絞り込みを外すか、「使えないものも表示」で理由を確かめてください。",
		facets: gentestSource("", gentest.MediaImage, nil).facets,
		list:   settingsImageCards,
	}
	sources[SourceUpscale] = source{
		title: "大きくするモデル", empty: "条件に合うモデルがありません。絞り込みを外すか、モデルマネージャーでアップスケール用のモデルを導入してください。",
		facets: func(context.Context, Services) modelpick.Facets {
			return modelpick.Facets{Commercial: true, Available: true}
		},
		list: func(ctx context.Context, svc Services, _ url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
			if svc.Gentest == nil {
				return nil, pickfilter.Counts{}, nil
			}
			if err := cond.Validate(); err != nil {
				return nil, pickfilter.Counts{}, err
			}
			all := svc.Gentest.Upscalers(ctx)
			var cards []modelcard.Card
			for _, u := range modelfilter.Filter(cond, all, modelcatalog.Upscaler.Item) {
				cards = append(cards, modelcards.Choose(modelcards.FromUpscaler(u), map[string]string{"value": u.ID}))
			}
			return cards, pickfilter.CountOf(cond, all, modelcatalog.Upscaler.Item), nil
		},
	}
	sources[SourceTrial] = source{
		title: "試すモデル", empty: "条件に合うモデルがありません。絞り込みを外すか、モデルマネージャーでモデルを導入してください。",
		facets: func(ctx context.Context, svc Services) modelpick.Facets {
			f := modelcards.Facets(gentest.MediaImage, svc.allModels(ctx))
			f.Kinds = []modelfilter.Kind{modelfilter.KindImage, modelfilter.KindVideo, modelfilter.KindImageEdit}
			f.Inputs = append(f.Inputs, modelcards.Facets(gentest.MediaVideo, nil).Inputs...)
			f.Audio = true
			return f
		},
		list: func(ctx context.Context, svc Services, _ url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
			return cardsOf(trialModels(ctx, svc), cond, modelcatalog.Info.Item)
		},
	}
}

// allModels are the generation test's models of both media (nil without the service).
func (svc Services) allModels(ctx context.Context) []modelcatalog.Info {
	if svc.Gentest == nil {
		return nil
	}
	return append(svc.Gentest.Models(ctx, gentest.MediaImage), svc.Gentest.Models(ctx, gentest.MediaVideo)...)
}

// trialModels are the models a trial can run: those the model manager lists for it (the models of the running setting).
func trialModels(ctx context.Context, svc Services) []modelcatalog.Info {
	if svc.ModelManager == nil {
		return nil
	}
	var ids []string
	for _, m := range mmview.TrialModels(svc.ModelManager) {
		ids = append(ids, m.ID)
	}
	var out []modelcatalog.Info
	for _, m := range svc.allModels(ctx) {
		if slices.Contains(ids, m.ID) {
			out = append(out, m)
		}
	}
	return out
}

// inpaintOnly says a model that cannot redraw part of a picture cannot be chosen there: it is listed with the reason.
func inpaintOnly(m modelcatalog.Info) *modelcard.Reason {
	if m.Available && !m.SupportsInpaint {
		return &modelcard.Reason{Text: notInpaint}
	}
	return nil
}

// gentestSource is a source of the generation test's models of a media. unsuited, when it gives a reason for a model, makes the model one that cannot be chosen there.
func gentestSource(title, media string, unsuited func(modelcatalog.Info) *modelcard.Reason) source {
	return source{
		title: title, empty: "条件に合うモデルがありません。絞り込みを外すか、「使えないものも表示」で理由を確かめてください。",
		facets: func(ctx context.Context, svc Services) modelpick.Facets {
			if svc.Gentest == nil {
				return modelcards.Facets(media, nil)
			}
			return modelcards.Facets(media, svc.Gentest.Models(ctx, media))
		},
		list: func(ctx context.Context, svc Services, _ url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
			if svc.Gentest == nil {
				return nil, pickfilter.Counts{}, nil
			}
			models := svc.Gentest.Models(ctx, media)
			item := modelcatalog.Info.Item
			if unsuited != nil {
				item = func(m modelcatalog.Info) modelfilter.Item {
					it := m.Item()
					if r := unsuited(m); r != nil {
						it.Available, it.Reason = false, r.Text
					}
					return it
				}
			}
			cards, counts, err := cardsOf(models, cond, item)
			if err != nil || unsuited == nil {
				return cards, counts, err
			}
			byID := map[string]*modelcard.Reason{}
			for _, m := range models {
				byID[m.ID] = unsuited(m)
			}
			for i := range cards {
				if r := byID[cards[i].Pick.Values["value"]]; r != nil {
					cards[i].Reason = r
				}
			}
			return cards, counts, nil
		},
	}
}

// cardsOf narrows the models by the condition (modelfilter) and makes each a card that chooses it ("value" is its id).
func cardsOf(models []modelcatalog.Info, cond modelfilter.Condition, item func(modelcatalog.Info) modelfilter.Item) ([]modelcard.Card, pickfilter.Counts, error) {
	if err := cond.Validate(); err != nil {
		return nil, pickfilter.Counts{}, err
	}
	passing := modelfilter.Filter(cond, models, item)
	cards := make([]modelcard.Card, len(passing))
	for i, m := range passing {
		cards[i] = modelcards.Choose(modelcards.FromInfo(m), map[string]string{"value": m.ID})
	}
	return cards, pickfilter.CountOf(cond, models, item), nil
}
