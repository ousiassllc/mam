package genplan_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// A route without an alternative model (a Config built in code, not loaded: the loader refuses it) names no model, so it must not add a line "動画: " to the
// models whose price is not known.
func TestUnpricedRoutes_ARouteWithoutAnAlternativeAddsNoEmptyName(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	route := cfg.Policies[domain.QualityFast]
	route.Fallback = modeladapter.VideoTarget{}
	cfg.Policies[domain.QualityFast] = route

	for _, name := range genplan.UnpricedRoutes(cfg, domain.QualityFast) {
		assert.NotEqual(t, "動画: ", name, "no model, no line")
	}
}
