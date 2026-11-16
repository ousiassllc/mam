package modelroute

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// audioReason is the sentence of a model chosen for a Shot with audio in place of the assignment's.
const audioReason = "この Shot は音声ファイルを使うため、音声を入力に取る利用可能なモデルを選びました"

// audioVideo is Video for a Shot with a sound file (Issue #1153; spec/architecture/model-adapters.md §3.1). A model that takes no sound would drop it without a word and
// make a clip that ignores it, so the Shot goes only to a model that takes it and that has what it needs (the Keyframe or a reference image: fitsVideo):
//
//   - The assignment's models that can be used and fit keep the route (an assignment that names a speech-to-video model is honoured: the first fitting one is primary, the second
//     the alternative). The ones that cannot be used or do not fit are passed over, each with its reason.
//   - Otherwise, a default assignment is replaced by the first candidate that fits, as an unusable one is. The sound is the Shot's own, more specific than the Quality Policy's
//     assignment, so it outranks it; the reason says so and the Take records it. The candidates are the usable models of the list that fit, in the order of the selection rule.
//   - A Video Model the user fixed for the Shot, or an assignment the operator pinned, is never replaced: the refusal names why each model does not fit and suggests the
//     candidates.
//   - With no model that fits the refusal (NoUsableError.Audio) names what is missing: a model that takes a sound, and the picture it needs.
func (e Env) audioVideo(q domain.QualityPolicy, vn VideoNeed, route modeladapter.PolicyRoute, pair []domain.ModelName) (VideoPick, error) {
	pick := VideoPick{Policy: q, Route: route, Selected: Selected{Selection: modelreg.SelectionAssignment, Reason: assignmentReason}}
	targets := []modeladapter.VideoTarget{route.Primary, route.Fallback}
	if vn.Forced != "" {
		targets = []modeladapter.VideoTarget{{Model: vn.Forced, Resolution: route.Primary.Resolution}}
	}
	needOf := func(res string) need {
		return need{video: true, res: res, seconds: vn.Seconds, keyframe: vn.Keyframe, audio: true, reference: vn.Reference, assigned: pair}
	}
	var passed []Problem
	var fit []modeladapter.VideoTarget
	for i, t := range targets {
		if i > 0 && t.Model == targets[0].Model {
			break
		}
		if av := e.check(t.Model); !av.Available {
			passed = append(passed, Problem{Model: t.Model, Message: av.Message, Route: av.Route})
		} else if why := e.fits(t.Model, needOf(t.Resolution)); why != "" {
			passed = append(passed, Problem{Model: t.Model, Message: why})
		} else {
			fit = append(fit, t)
		}
	}
	if len(fit) > 0 {
		pick.Route = modeladapter.PolicyRoute{Primary: fit[0], Fallback: fit[len(fit)-1], Takes: route.Takes}
		return pick, nil
	}
	n := needOf(route.Primary.Resolution)
	cands, unmatched := e.candidates(domain.VideoModels(), n)
	if vn.Forced != "" || e.Pinned || len(cands) == 0 {
		return pick, &NoUsableError{Media: "video", Key: string(q), Audio: true, Assigned: passed, Explicit: vn.Forced != "" || e.Pinned, Pinned: e.Pinned, Candidates: cands, Unmatched: unmatched}
	}
	t := modeladapter.VideoTarget{Model: cands[0].Model, Resolution: route.Primary.Resolution}
	pick.Route = modeladapter.PolicyRoute{Primary: t, Fallback: t, Takes: route.Takes}
	pick.Selected = Selected{Selection: modelreg.SelectionCondition, Reason: audioReason, Passed: passed}
	return pick, nil
}
