package generation

import (
	"math"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
)

// routes is what the models of a generation are chosen by: the running assignment judged against what can be used now (spec/architecture/model-adapters.md §3.1).
func (o *Orchestrator) routes() modelroute.Env { return modelroute.ForAdapter(o.adapter) }

// checkPackModels refuses a Reference Pack that has no model to make its images with: an adult model of the assignment while the age confirmation is off
// (*agegate.RefusedError), or an image purpose none of whose models can be used now when none may be chosen in its place (modelroute.ErrNoUsableModel). It
// reads state only, so a start asks it before the job is queued; the job chooses again as it makes each image. It returns the least one image of the pack costs
// (0 when a model of the chosen routes is free or has no price: an unknown price is not counted against a ceiling) and the models the progress card names.
func (o *Orchestrator) checkPackModels() (cheapest float64, models string, err error) {
	env, cheapest := o.routes().Memoised(), math.Inf(1)
	for _, p := range []modeladapter.ImagePurpose{modeladapter.PurposeNewReference, modeladapter.PurposeEditReference} {
		pick, err := env.Image(p)
		if err != nil {
			return 0, "", err
		}
		cheapest = min(cheapest, modelroute.EstimateImage(env.Config, pick.Route))
		models += "、" + string(p) + "＝" + env.Named(pick.Selected, pick.Route.Primary.Model, pick.Route.Fallback.Model, "image")
	}
	return cheapest, "使う画像モデル: " + strings.TrimPrefix(models, "、"), nil
}

// videoModels is the models the progress card of a generation names: the one that is tried first (the alternative when the primary cannot be used), with its route and why it was chosen.
func (o *Orchestrator) videoModels(s Strategy) string {
	return "使う動画モデル: " + o.routes().Named(s.Models, s.Primary.Model, s.Fallback.Model, "video")
}
