package modelpicker

import (
	"context"
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/loras"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// SourceLoRA is the LoRA file of the generation test's LoRA choice (#1011, #1119). It is chosen as a model is (the same bar, panel, cards and badges), but a LoRA
// has no kind, input, size or VRAM: the panel offers only the commercial-use choice (what the user confirmed for the file) and the search, which reads the name,
// the base model and the trigger words. The first card is "LoRA を使わない"; it is a choice, not a model, so the filters do not hide it.
const SourceLoRA = "lora"

// QueryMedia is the context the field of the LoRA carries: the media the generation test form is for (the engine's LoRAs are for images).
const QueryMedia = "media"

func init() {
	sources[SourceLoRA] = source{
		title: "LoRA", keepAll: true, empty: "条件に合う LoRA がありません。絞り込みを外してください。",
		facets: func(context.Context, Services) modelpick.Facets { return modelpick.Facets{Commercial: true} },
		list: func(ctx context.Context, svc Services, q url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
			if svc.Gentest == nil {
				return nil, pickfilter.Counts{}, nil
			}
			if err := cond.Validate(); err != nil {
				return nil, pickfilter.Counts{}, err
			}
			all := svc.Gentest.LoRAChoices(ctx, q.Get(QueryMedia))
			cards := []modelcard.Card{modelcards.NoLoRA()}
			for _, c := range modelfilter.Filter(cond, all, loras.Choice.Item) {
				cards = append(cards, modelcards.FromLoRA(c))
			}
			return cards, pickfilter.CountOf(cond, all, loras.Choice.Item), nil
		},
	}
}
