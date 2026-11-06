package configcheck

import (
	"log/slog"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// warnUnpricedGPTImage2 says so when the price of GPT Image 2 is unknown: its API model ID is not gpt-image-2 (or a
// snapshot of it) and no cost_per_image_usd is set. The table of gptimage is that model's, so the estimate is 0
// then, the budget check would not hold the model at all and the cost ledger would record no estimate (spec/
// architecture/model-adapters.md §9.2). It is a warning, not an error: pricing another model by hand is allowed.
func warnUnpricedGPTImage2(c modeladapter.Config) {
	mc := c.ModelSettings(domain.ModelGPTImage2)
	if mc.CostPerImage == 0 && !gptimage.Priced(mc.Model) {
		slog.Warn("models.gpt-image-2.model is not gpt-image-2 and has no cost_per_image_usd: its image price is unknown and counts as 0 in the budget check; set cost_per_image_usd",
			"model", mc.Model)
	}
}

// warnUnpricedGPTImage26 says so when a GPT Image 2.6 model that a route uses has no cost_per_image_usd: OpenAI publishes no price
// per image for it (spec/architecture/model-adapters.md §9.5), so its estimate is 0, the budget check does not hold it and the cost
// ledger records the real cost (from the usage of the response) only after the call. It is a warning: the default setting is in this case.
func warnUnpricedGPTImage26(c modeladapter.Config) {
	for _, m := range []domain.ModelName{domain.ModelGPTImage26Flare, domain.ModelGPTImage26Sunburst} {
		if c.ModelSettings(m).CostPerImage == 0 && routed(c, m) {
			slog.Warn("a GPT Image 2.6 model is routed and has no cost_per_image_usd: OpenAI publishes no price per image for it, so the budget check does not count it before the call; set models.<model>.cost_per_image_usd",
				"model", m)
		}
	}
}

func routed(c modeladapter.Config, m domain.ModelName) bool {
	for _, r := range c.Images {
		if r.Primary.Model == m || r.Fallback.Model == m {
			return true
		}
	}
	return false
}
