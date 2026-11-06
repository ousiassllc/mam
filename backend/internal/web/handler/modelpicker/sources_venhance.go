package modelpicker

import (
	"context"
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelcards"
	"github.com/ousiassllc/mam/backend/internal/web/handler/pickfilter"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelpick"
)

// The context the field of the video enhancement carries (the box has a form for each operation and for each of the two places it runs): the operation
// (upscale / interpolate) and "1" for the models of Topaz (the paid cloud) rather than those of this PC.
const (
	QueryOp    = "op"
	QueryCloud = "cloud"
)

// SourceVideoEnhance is the model of "make the video larger / smoother" of a result's detail.
const SourceVideoEnhance = "video-enhance"

func init() {
	sources[SourceVideoEnhance] = source{
		title: "動画を大きくする・滑らかにするモデル", empty: "条件に合うモデルがありません。絞り込みを外すか、「使えないものも表示」で理由を確かめてください。",
		facets: func(context.Context, Services) modelpick.Facets {
			return modelpick.Facets{Commercial: true, Available: true}
		},
		list: func(ctx context.Context, svc Services, q url.Values, cond modelfilter.Condition) ([]modelcard.Card, pickfilter.Counts, error) {
			if svc.Gentest == nil {
				return nil, pickfilter.Counts{}, nil
			}
			if err := cond.Validate(); err != nil {
				return nil, pickfilter.Counts{}, err
			}
			var all []modelcatalog.VideoEnhancer
			for _, m := range videoenhance.Enhancers(ctx, svc.Gentest) {
				if m.Op == q.Get(QueryOp) && m.Paid == (q.Get(QueryCloud) == "1") {
					all = append(all, m)
				}
			}
			var cards []modelcard.Card
			for _, m := range modelfilter.Filter(cond, all, modelcatalog.VideoEnhancer.Item) {
				cards = append(cards, modelcards.Choose(modelcards.FromVideoEnhancer(m), map[string]string{"value": m.ID}))
			}
			return cards, pickfilter.CountOf(cond, all, modelcatalog.VideoEnhancer.Item), nil
		},
	}
}
