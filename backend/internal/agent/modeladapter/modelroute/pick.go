package modelroute

import (
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// Selected says which models a generation runs with and why: the assignment's, or a model chosen because it matches the conditions.
type Selected struct {
	Selection modelreg.Selection
	// Reason is the selection in one sentence for the user.
	Reason string
	// Passed are the models of the assignment that were passed over and why (empty while the assignment is used).
	Passed []Problem
}

// Substituted reports whether a model was chosen in place of the assignment.
func (s Selected) Substituted() bool { return s.Selection == modelreg.SelectionCondition }

const (
	assignmentReason = "割り当てのモデルです"
	fellBackReason   = "割り当ての予備のモデルで作りました（主のモデルが使えないか、失敗したため）"
)

// conditionReason is the sentence of a model chosen in place of the assignment. It claims a match of conditions, never a better model.
const conditionReason = "既定の割当が使えないため、条件に合う利用可能なモデルを選びました"

// Detail is Reason with why each model of the assignment was passed over, for the record of the Prompt version.
func (s Selected) Detail() string {
	parts := make([]string, len(s.Passed))
	for i, p := range s.Passed {
		parts[i] = string(p.Model) + "＝" + p.Message
	}
	return s.Reason + "（割当が使えない理由: " + strings.Join(parts, "、") + "）"
}

// Use is what Selected says of the model that made an output: its route, its cost basis and the reason. fellBack is the Adapter's own fail-over to the
// assignment's alternative model.
func (s Selected) Use(cfg modeladapter.Config, m domain.ModelName, media string, fellBack bool) modelreg.Use {
	u := modelreg.Use{
		Model: string(m), Route: modelreg.RouteOf(string(m)), Selection: s.Selection, Reason: s.Reason,
		CostBasis: BasisOf(cfg, m, media),
	}
	for _, a := range s.Passed {
		u.Passed = append(u.Passed, modelreg.Passed{Model: string(a.Model), Why: a.Message})
	}
	if u.Selection == "" {
		u.Selection, u.Reason = modelreg.SelectionAssignment, assignmentReason
	}
	if u.Selection == modelreg.SelectionAssignment && fellBack {
		u.Reason = fellBackReason
	}
	return u
}

// EstimateImage is the most one image of the route can cost: the dearer of its primary model and the alternative the call falls back to. It is what a
// budget check holds before the call.
func EstimateImage(cfg modeladapter.Config, r modeladapter.ImageRoute) float64 {
	return max(cfg.ImageCost(r.Primary.Model), cfg.ImageCost(r.Fallback.Model))
}

// ImagePick is the route an image of a purpose is made with.
type ImagePick struct {
	Purpose modeladapter.ImagePurpose
	// Route is what to call: the assignment's, or the chosen model alone (no alternative: a failure stops there instead of moving to a model of another
	// route and another cost).
	Route modeladapter.ImageRoute
	Selected
}

// Image chooses the models for an image of the purpose. It returns *NoUsableError (ErrNoUsableModel) when none of the assignment can be used and none may be
// chosen in its place.
func (e Env) Image(p modeladapter.ImagePurpose) (ImagePick, error) {
	route := e.Config.ImageRouteFor(p)
	pair := pairOf(route.Primary.Model, route.Fallback.Model)
	pick := ImagePick{Purpose: p, Route: route, Selected: Selected{Selection: modelreg.SelectionAssignment, Reason: assignmentReason}}
	if err := e.refuse(pair); err != nil {
		return pick, err
	}
	problems, ok := e.problems(pair)
	if ok {
		return pick, nil
	}
	n := need{refs: p == modeladapter.PurposeEditReference, assigned: pair}
	cands, unmatched := e.candidates(domain.ImageModels(), n)
	explicit := e.explicitImage(p)
	if explicit || len(cands) == 0 {
		return pick, &NoUsableError{Media: "image", Key: string(p), Assigned: problems, Explicit: explicit, Pinned: e.Pinned, Candidates: cands, Unmatched: unmatched}
	}
	m := cands[0].Model
	pick.Route = modeladapter.ImageRoute{Primary: modeladapter.ImageTarget{Model: m}, Fallback: modeladapter.ImageTarget{Model: m}}
	pick.Selected = Selected{Selection: modelreg.SelectionCondition, Reason: conditionReason, Passed: problems}
	return pick, nil
}

// VideoNeed is what a Shot asks of the video model besides its Quality Policy.
type VideoNeed struct {
	// Seconds is the length of the Shot; the model must hold it in one clip (0: no condition).
	Seconds float64
	// Keyframe is whether the Shot has a Keyframe, the first frame of the clip: the model must take a first frame, and a model that cannot run without
	// one needs it. A Shot of the AUTO flow has one (its Reference Pack is built first), so the report and the plan, which are not about one Shot, assume it.
	Keyframe bool
	// Forced is the Video Model the user fixed for the Shot (a MANUAL or LOCK); it is never replaced.
	Forced domain.ModelName
}

// VideoPick is the route a Shot's video is made with.
type VideoPick struct {
	Policy domain.QualityPolicy
	// Route is the Quality Policy's route, or the chosen model alone at the Policy's resolution.
	Route modeladapter.PolicyRoute
	Selected
}

// Video chooses the models for the video of a Shot under the Quality Policy; see Image for the error.
func (e Env) Video(q domain.QualityPolicy, vn VideoNeed) (VideoPick, error) {
	route, ok := e.Config.Policies[q]
	pick := VideoPick{Policy: q, Route: route, Selected: Selected{Selection: modelreg.SelectionAssignment, Reason: assignmentReason}}
	if !ok {
		return pick, nil // Decide says there is no route for the Quality Policy
	}
	pair := pairOf(route.Primary.Model, route.Fallback.Model)
	explicit := e.explicitVideo(q)
	if vn.Forced != "" {
		pair, explicit = []domain.ModelName{vn.Forced}, true
	}
	if err := e.refuse(pair); err != nil {
		return pick, err
	}
	problems, usable := e.problems(pair)
	if usable {
		return pick, nil
	}
	n := need{video: true, res: route.Primary.Resolution, seconds: vn.Seconds, keyframe: vn.Keyframe, assigned: pair}
	cands, unmatched := e.candidates(domain.VideoModels(), n)
	if explicit || len(cands) == 0 {
		return pick, &NoUsableError{Media: "video", Key: string(q), Assigned: problems, Explicit: explicit, Pinned: e.Pinned, Candidates: cands, Unmatched: unmatched}
	}
	t := modeladapter.VideoTarget{Model: cands[0].Model, Resolution: route.Primary.Resolution}
	pick.Route = modeladapter.PolicyRoute{Primary: t, Fallback: t, Takes: route.Takes}
	pick.Selected = Selected{Selection: modelreg.SelectionCondition, Reason: conditionReason, Passed: problems}
	return pick, nil
}

// refuse is the age gate over the models the request is about: the assignment or the Video Model the user fixed, not the models that might replace it.
func (e Env) refuse(models []domain.ModelName) error {
	if e.Refuse == nil {
		return nil
	}
	return e.Refuse(models...)
}

// pairOf is the models of a route, once each.
func pairOf(primary, fallback domain.ModelName) []domain.ModelName {
	if primary == fallback {
		return []domain.ModelName{primary}
	}
	return []domain.ModelName{primary, fallback}
}

// Problem is a model of the assignment that cannot be used now, why, and the screen that fixes it.
type Problem struct {
	Model   domain.ModelName `json:"model"`
	Message string           `json:"message"`
	Route   string           `json:"settings_route,omitempty"`
}

// problems judges the models of a route: ok is true when some of them can be used (the Adapter moves on to it by itself), else problems says why each cannot.
func (e Env) problems(models []domain.ModelName) (out []Problem, ok bool) {
	for _, m := range models {
		av := e.check(m)
		if av.Available {
			return nil, true
		}
		out = append(out, Problem{Model: m, Message: av.Message, Route: av.Route})
	}
	return out, false
}

// explicitImage reports whether the assignment of the purpose is the user's: the operator pinned the file, the user switched to the OpenAI route, or the
// purpose's route is not the default one.
func (e Env) explicitImage(p modeladapter.ImagePurpose) bool {
	if e.Pinned || (e.Config.PreferOpenAI && p == modeladapter.PurposeNewReference) {
		return true
	}
	return e.Config.Images[p] != modeladapter.DefaultConfig().Images[p]
}

// explicitVideo reports whether the assignment of the Quality Policy is the user's: the operator pinned the file or the models of the route (primary and
// alternative, with their resolution) are not the default ones. The number of Takes is not a choice of model, so changing it alone changes nothing here.
func (e Env) explicitVideo(q domain.QualityPolicy) bool {
	r, d := e.Config.Policies[q], modeladapter.DefaultConfig().Policies[q]
	return e.Pinned || r.Primary != d.Primary || r.Fallback != d.Fallback
}

// BasisOf is how the price of the model is known: a model on this PC is free unless the user priced it, the ChatGPT subscription's model has no price (the plan
// pays: not free, and not counted), and an API model is priced by the settings or unknown.
func BasisOf(cfg modeladapter.Config, m domain.ModelName, media string) modelreg.CostBasis {
	if modelreg.RouteOf(string(m)) == modelreg.RouteSubscription {
		return modelreg.CostUnknown
	}
	local := modelreg.RouteOf(string(m)) == modelreg.RouteLocal
	if media == "image" {
		switch {
		case cfg.ImageCost(m) > 0:
			return modelreg.CostPriced
		case local:
			return modelreg.CostFree
		}
		return modelreg.CostUnknown
	}
	caps, _ := cfg.VideoCaps(m)
	if slices.ContainsFunc(caps.Resolutions, func(r string) bool {
		return cfg.VideoPerSecond(modeladapter.VideoTarget{Model: m, Resolution: r}) > 0
	}) {
		return modelreg.CostPriced
	}
	if local {
		return modelreg.CostFree
	}
	return modelreg.CostUnknown
}
