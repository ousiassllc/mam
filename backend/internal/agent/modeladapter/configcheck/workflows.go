package configcheck

import (
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// withWorkflows attaches the published saved workflows (localcfg.PublishWorkflows) to cfg and points the routes that
// name an older version of a workflow at its latest: a route means "use this workflow", while a Shot's Video Model
// lock pins an exact version.
func withWorkflows(cfg modeladapter.Config) modeladapter.Config {
	cfg.Workflows = localcfg.PublishedWorkflows()
	if len(cfg.Workflows) == 0 {
		return cfg
	}
	latest := func(m domain.ModelName) domain.ModelName { return domain.ModelName(localcfg.Latest(string(m))) }
	cfg.Images = maps.Clone(cfg.Images)
	for p, r := range cfg.Images {
		r.Primary.Model, r.Fallback.Model = latest(r.Primary.Model), latest(r.Fallback.Model)
		cfg.Images[p] = r
	}
	cfg.Policies = maps.Clone(cfg.Policies)
	for q, r := range cfg.Policies {
		r.Primary.Model, r.Fallback.Model = latest(r.Primary.Model), latest(r.Fallback.Model)
		cfg.Policies[q] = r
	}
	cfg.Routing.Candidates = maps.Clone(cfg.Routing.Candidates)
	for q, targets := range cfg.Routing.Candidates {
		moved := slices.Clone(targets)
		for i := range moved {
			moved[i].Model = latest(moved[i].Model)
		}
		cfg.Routing.Candidates[q] = moved
	}
	return cfg
}
