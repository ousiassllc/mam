package genplan_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/genplan"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
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

// unpricedVideo is DefaultConfig with the price of the primary video model of the policy taken away, and the name the notice gives that model.
func unpricedVideo(q domain.QualityPolicy) (modeladapter.Config, string) {
	cfg := modeladapter.DefaultConfig()
	model := cfg.Policies[q].Primary.Model
	mc := cfg.Models[model]
	mc.CostPerSecond = nil
	cfg.Models[model] = mc
	return cfg, "動画: " + string(model)
}

// A video model of the policy that has no price is named.
func TestUnpricedRoutes_AVideoModelWithoutAPriceIsNamed(t *testing.T) {
	cfg, name := unpricedVideo(domain.QualityBalanced)
	assert.NotContains(t, genplan.UnpricedRoutes(modeladapter.DefaultConfig(), domain.QualityBalanced), name, "the same model with its price is not unpriced")
	assert.Contains(t, genplan.UnpricedRoutes(cfg, domain.QualityBalanced), name)
}

// The primary and the alternative of a route can be the same model: it is named once.
func TestUnpricedRoutes_AModelThatIsBothPrimaryAndAlternativeIsNamedOnce(t *testing.T) {
	cfg, name := unpricedVideo(domain.QualityBalanced)
	route := cfg.Policies[domain.QualityBalanced]
	route.Fallback = route.Primary
	cfg.Policies[domain.QualityBalanced] = route

	got := genplan.UnpricedRoutes(cfg, domain.QualityBalanced)
	assert.Len(t, slices.DeleteFunc(slices.Clone(got), func(s string) bool { return s != name }), 1, "named once")
}

// A Project with no Shot to make has no plan to read the models from: the routes of its Quality Policy tell.
func TestBuilderUnpricedModels_WithoutPlannedShotsItReadsTheRoutesOfThePolicy(t *testing.T) {
	db := testdb.Open(t)
	id := domain.NewID()
	require.NoError(t, repository.NewProjectRepository(db).Create(context.Background(), repository.NewProject{
		Project: domain.Project{ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmFullAuto, ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced},
	}))
	cfg, name := unpricedVideo(domain.QualityBalanced)
	b := genplan.Builder{
		Plans: repository.NewPlanRepository(db), Gens: repository.NewGenerationRepository(db), Policies: repository.NewPolicyRepository(db),
		Config: func() modeladapter.Config { return cfg },
	}

	got, err := b.UnpricedModels(context.Background(), id, nil)
	require.NoError(t, err)
	assert.Equal(t, genplan.UnpricedRoutes(cfg, domain.QualityBalanced), got)
	assert.Contains(t, got, name)
}
