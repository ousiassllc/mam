package modelroute

import (
	"math"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
)

// PackPreflight is the check a Reference Pack passes before it starts (and before an automatic start of it): it refuses a pack that has no model to make its images
// with: an adult model of the assignment while the age confirmation is off (*agegate.RefusedError), or an image purpose none of whose models can be used now when
// none may be chosen in its place (ErrNoUsableModel, with the cause and the ways out: register a key, sign in, install a local model, change the assignment). It
// reads state only (no network, no cost, no write), so a start asks it before the job is queued; the job chooses again as it makes each image. It returns the least one
// image of the pack costs, by the model actually chosen (0 when a model of the chosen routes is free or has no price: an unknown price is not counted against a ceiling),
// and the models the progress card names, each with its route and why it was chosen (#1112).
func (e Env) PackPreflight() (cheapest float64, models string, err error) {
	e, cheapest = e.Memoised(), math.Inf(1)
	for _, p := range []modeladapter.ImagePurpose{modeladapter.PurposeNewReference, modeladapter.PurposeEditReference} {
		pick, err := e.Image(p)
		if err != nil {
			return 0, "", err
		}
		cheapest = min(cheapest, EstimateImage(e.Config, pick.Route))
		models += "、" + string(p) + "＝" + e.Named(pick.Selected, pick.Route.Primary.Model, pick.Route.Fallback.Model, "image")
	}
	return cheapest, "使う画像モデル: " + strings.TrimPrefix(models, "、"), nil
}
