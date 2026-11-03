package modelpicker

import (
	"context"
	"net/url"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/commercial"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// ChoiceValue is the value of a video choice of a Quality Policy: the model at a resolution ("<model>@<resolution>", what the form posts).
func ChoiceValue(model, resolution string) string { return model + "@" + resolution }

func init() {
	sources[SourceSettingsVideo] = source{
		title: "動画モデル（解像度つき）", empty: "条件に合うモデルがありません。絞り込みを外すか、「使えないものも表示」で理由を確かめてください。",
		facets: func(ctx context.Context, svc Services) modelpick.Facets {
			if svc.Gentest == nil {
				return modelcards.Facets(gentest.MediaVideo, nil)
			}
			return modelcards.Facets(gentest.MediaVideo, svc.Gentest.Models(ctx, gentest.MediaVideo))
		},
		list: videoChoices,
	}
	sources[SourceLLM] = source{
		title: "LLM のモデル", empty: "条件に合うモデルがありません。絞り込みを外すか、「使えないものも表示」で理由を確かめてください。",
		facets: func(context.Context, Services) modelpick.Facets {
			var providers []modelpick.Option
			for _, p := range llmmodels.ProviderNames() {
				providers = append(providers, modelpick.Option{Value: p, Label: modelpick.ProviderLabel(p)})
			}
			return modelpick.Facets{Origins: true, Providers: providers, Available: true}
		},
		list: llmCards,
	}
}

// videoChoices are the video models of a Quality Policy, one card for each resolution the assignment may use with a model (modelsettings.VideoOption): the models that
// pass the condition by their generation-test description, so that the same filters tell them apart as everywhere.
func videoChoices(ctx context.Context, svc Services, _ url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
	if svc.Settings == nil {
		return nil, pickfilter.Counts{}, nil
	}
	view, err := svc.Settings.View()
	if err != nil {
		return nil, pickfilter.Counts{}, nil //nolint:nilerr // a file that cannot be read has no choices to offer; the settings screen says so
	}
	if err := cond.Validate(); err != nil {
		return nil, pickfilter.Counts{}, err
	}
	if svc.Gentest == nil { // no description of the models: the choices as the settings know them
		var cards []modelcard.Card
		for _, o := range view.VideoModelsWhere(cond) {
			for _, res := range o.Resolutions {
				cards = append(cards, modelcards.Choose(plainCard(string(o.Model), gentest.MediaVideo, o.Availability, res), map[string]string{"value": ChoiceValue(string(o.Model), res)}))
			}
		}
		return cards, pickfilter.CountOf(cond, view.VideoModels, modelsettings.VideoOption.Item), nil
	}
	models := svc.Gentest.Models(ctx, gentest.MediaVideo)
	passing := modelfilter.Filter(cond, models, modelcatalog.Info.Item)
	var cards []modelcard.Card
	for _, m := range passing {
		for _, o := range view.VideoModels {
			if string(o.Model) != m.ID {
				continue
			}
			for _, res := range o.Resolutions {
				c := modelcards.FromInfo(m)
				c.ID, c.Name = "mp-"+m.ID+"@"+res, m.Label+"（"+res+"）"
				cards = append(cards, modelcards.Choose(c, map[string]string{"value": ChoiceValue(m.ID, res)}))
			}
		}
	}
	return cards, pickfilter.CountOf(cond, models, modelcatalog.Info.Item), nil
}

// llmCards are the models of the providers (llmmodels.Candidates), each choosing the provider and the model together.
func llmCards(ctx context.Context, svc Services, _ url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
	if svc.LLM == nil {
		return nil, pickfilter.Counts{}, nil
	}
	if err := cond.Validate(); err != nil {
		return nil, pickfilter.Counts{}, err
	}
	all, err := svc.LLM.Candidates(ctx, modelfilter.Condition{})
	if err != nil {
		return nil, pickfilter.Counts{}, err
	}
	passing := modelfilter.Filter(cond, all, llmmodels.Candidate.Item)
	cards := make([]modelcard.Card, len(passing))
	for i, c := range passing {
		cards[i] = modelcards.Choose(LLMCard(c.Provider, c.Model.ID, c.Model.Name, c.Availability), map[string]string{"provider": c.Provider, "model": c.Model.ID})
	}
	return cards, pickfilter.CountOf(cond, all, llmmodels.Candidate.Item), nil
}

// LLMCard is the card of an LLM: the model (its display name, else its id) and the provider; why it cannot be used when the provider has no key or no sign-in.
func LLMCard(provider, id, name string, a modelfilter.Availability) modelcard.Card {
	if name == "" {
		name = id
	}
	c := modelcard.Card{
		ID: "mp-" + provider + "-" + id, Name: name, Icon: atoms.IconText, Meta: modelpick.ProviderLabel(provider) + " ・ " + id,
		Reason: modelcards.Unavailable(a),
	}
	if name == id {
		c.Meta = modelpick.ProviderLabel(provider)
	}
	return c
}

// settingsImageCards are the image models an image purpose of the settings may be assigned (modelsettings.View.ImageModels: those the adapter can run, the age gate's
// hidden ones left out), described by the generation test so that the same filters tell them apart as everywhere.
func settingsImageCards(ctx context.Context, svc Services, _ url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
	if svc.Settings == nil {
		return nil, pickfilter.Counts{}, nil
	}
	view, err := svc.Settings.View()
	if err != nil {
		return nil, pickfilter.Counts{}, nil //nolint:nilerr // a file that cannot be read has no choices to offer; the settings screen says so
	}
	if err := cond.Validate(); err != nil {
		return nil, pickfilter.Counts{}, err
	}
	if svc.Gentest == nil { // no description of the models: the choices as the settings know them
		var cards []modelcard.Card
		for _, o := range view.ImageModelsWhere(cond) {
			cards = append(cards, modelcards.Choose(plainCard(string(o.Model), gentest.MediaImage, o.Availability, ""), map[string]string{"value": string(o.Model)}))
		}
		return cards, pickfilter.CountOf(cond, view.ImageModels, modelsettings.ImageOption.Item), nil
	}
	var models []modelcatalog.Info
	for _, m := range svc.Gentest.Models(ctx, gentest.MediaImage) {
		if slices.ContainsFunc(view.ImageModels, func(o modelsettings.ImageOption) bool { return string(o.Model) == m.ID }) {
			models = append(models, m)
		}
	}
	return cardsOf(models, cond, modelcatalog.Info.Item)
}

// plainCard is the card of a model the settings know only by its name (no generation test to describe it): the name (with the resolution of a video choice), its
// commercial-use answer and, when it cannot be used now, why.
func plainCard(model, media string, a modelfilter.Availability, resolution string) modelcard.Card {
	answer := commercial.Of(model)
	name, icon := model, atoms.IconImage
	if resolution != "" {
		name += "（" + resolution + "）"
	}
	if media == gentest.MediaVideo {
		icon = atoms.IconVideo
	}
	id := "mp-" + model
	if resolution != "" {
		id += "@" + resolution
	}
	return modelcard.Card{ID: id, Name: name, Icon: icon, Commercial: &answer, Reason: modelcards.Unavailable(a)}
}
