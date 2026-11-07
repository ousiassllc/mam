package genplan

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// usd writes an amount the way the screens do.
func usd(v float64) string { return fmt.Sprintf("%.4f", v) }

// numbers writes the Shot numbers as "1・2・3".
func numbers(nums []int) string {
	text := make([]string, len(nums))
	for i, n := range nums {
		text[i] = strconv.Itoa(n)
	}
	return strings.Join(text, "・")
}

// priced is the estimate of one call that costs c: a price above 0 is known, a local model without a price is free, anything else (a
// cloud model without a price in models.json, the ChatGPT subscription's model) is unknown. It never makes a price up.
func priced(cfg modeladapter.Config, m domain.ModelName, c float64) Cost {
	if chatgptid.Is(m) {
		return Cost{Unknown: true}
	}
	if _, _, local := cfg.LocalModel(m); c > 0 || local {
		return Cost{USD: c}
	}
	return Cost{Unknown: true}
}

// imageCost is the estimate of the most one image of the purpose costs (the dearer of its primary and alternative model, as the budget check
// holds it) and the models of the route.
func imageCost(cfg modeladapter.Config, purpose modeladapter.ImagePurpose) (Cost, []domain.ModelName) {
	route := cfg.ImageRouteFor(purpose)
	models := []domain.ModelName{route.Primary.Model}
	if route.Fallback.Model != route.Primary.Model {
		models = append(models, route.Fallback.Model)
	}
	var c Cost
	for _, m := range models {
		one := priced(cfg, m, cfg.ImageCost(m))
		c.Unknown = c.Unknown || one.Unknown
		c.USD = max(c.USD, one.USD)
	}
	return c, models
}

// packCost is the most one Reference Pack costs: the three identity images (a new image or an edit of the material the user gave, whichever
// is dearer) and the three edits (Style, Lighting, Keyframe). It also names the models that make them and the ones whose price is unknown.
func packCost(cfg modeladapter.Config) (c Cost, models string, unpriced []string) {
	fresh, freshModels := imageCost(cfg, modeladapter.PurposeNewReference)
	edit, editModels := imageCost(cfg, modeladapter.PurposeEditReference)
	identity := Cost{USD: max(fresh.USD, edit.USD), Unknown: fresh.Unknown || edit.Unknown}
	c = Cost{USD: float64(packImages/2)*identity.USD + float64(packImages/2)*edit.USD, Unknown: identity.Unknown || edit.Unknown}
	for _, m := range append(slices.Clone(freshModels), editModels...) {
		if priced(cfg, m, cfg.ImageCost(m)).Unknown && !slices.Contains(unpriced, string(m)) {
			unpriced = append(unpriced, string(m))
		}
	}
	return c, string(freshModels[0]) + " / " + string(editModels[0]), unpriced
}
