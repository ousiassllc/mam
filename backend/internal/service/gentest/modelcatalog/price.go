package modelcatalog

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// Price is the unit price of a model.
type Price struct {
	Basis string `json:"basis" jsonschema:"priced（単価あり）/ free（ローカル: 0）/ unknown（単価なし。見積りの合計に入れない）"`
	// PerImageUSD is the price of one image (an image model with Basis priced).
	PerImageUSD *float64 `json:"per_image_usd,omitempty"`
	// PerSecondUSD is the price of one second of video by resolution (a video model with Basis priced).
	PerSecondUSD map[string]float64 `json:"per_second_usd,omitempty"`
}

// priceOf is the unit price: a local model is free unless the user priced it, the ChatGPT subscription's model has no price
// (the plan pays), and a cloud model without a price in models.json is unknown, as is a video model the selected API provider does not offer.
func priceOf(cfg modeladapter.Config, m domain.ModelName, media string) Price {
	if chatgptid.Is(m) {
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
	if cfg.ProviderRefusal(m) != "" {
		// The selected provider does not offer the model (provider_unsupported): the maker's price is not what a run would cost.
		return Price{Basis: BasisUnknown}
	}
	caps, _ := cfg.VideoCaps(m)
	prices := map[string]float64{}
	for _, res := range caps.Resolutions {
		if p := cfg.VideoPerSecond(modeladapter.VideoTarget{Model: m, Resolution: res}); p > 0 {
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
