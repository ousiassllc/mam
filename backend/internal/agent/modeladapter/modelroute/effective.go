package modelroute

import (
	"maps"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Effective is the assignment as a generation would use it if it started now: every route the Env would replace by a model that matches the conditions
// is replaced here too, so that the plan and the estimate of a generation (genplan) name and price the models that will make it. A route that cannot be
// used and may not be replaced is left as it is (the start refuses it). The conditions that belong to one Shot (its length, its Keyframe) are not known
// here: a video route is judged for a Shot that has a Keyframe and by the Quality Policy's resolution alone, and a Shot whose length no chosen model holds is refused when it starts.
func (e Env) Effective() modeladapter.Config {
	e = e.Memoised()
	cfg := e.Config
	cfg.Images, cfg.Policies = maps.Clone(cfg.Images), maps.Clone(cfg.Policies)
	for _, p := range modeladapter.ImagePurposes() {
		if pick, err := e.Image(p); err == nil && pick.Substituted() {
			cfg.Images[p] = pick.Route
		}
	}
	for _, q := range domain.QualityPolicies() {
		if pick, err := e.Video(q, VideoNeed{Keyframe: true}); err == nil && pick.Substituted() {
			cfg.Policies[q] = pick.Route
		}
	}
	return cfg
}

// Effective is the function of the effective assignment of the Adapter, for the plan of a generation (genplan.Builder.Config).
func Effective(a *modeladapter.Adapter) func() modeladapter.Config {
	return func() modeladapter.Config { return ForAdapter(a).Effective() }
}
