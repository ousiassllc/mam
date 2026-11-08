package modelroute

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
)

// Route statuses (RouteReport.Status).
const (
	// StatusAssignment: the assignment can be used as it is.
	StatusAssignment = "assignment_usable"
	// StatusCondition: the assignment cannot be used, and a generation would run on a model that matches the conditions (a default assignment only).
	StatusCondition = "condition_match"
	// StatusBlocked: nothing can be used, and a generation would be refused with the cause and the ways out.
	StatusBlocked = "blocked"
)

// AssignedModel is a model of the assignment with whether it can be used now.
type AssignedModel struct {
	Role               string             `json:"role" jsonschema:"primary または fallback"`
	Model              string             `json:"model"`
	RouteKind          modelreg.RouteKind `json:"route_kind" jsonschema:"api / chatgpt-subscription / local"`
	Available          bool               `json:"available"`
	UnavailableMessage string             `json:"unavailable_message,omitempty" jsonschema:"使えない理由"`
	SettingsRoute      string             `json:"settings_route,omitempty" jsonschema:"直す画面の道すじ"`
}

// RouteReport is, for one route of the assignment, what a generation would run on if it started now: the one list the screens and the MCP tools show, built
// by the same rule as the generation (Env.Image, Env.Video), so that the two cannot differ. It is read-only.
type RouteReport struct {
	Media    string `json:"media" jsonschema:"image / video"`
	Key      string `json:"key" jsonschema:"画像の用途（new_reference など）か Quality Policy（fast など）"`
	Explicit bool   `json:"explicit" jsonschema:"割り当てが利用者のもの（既定から変えた・起動オプションで固定）。true の間は、使えなくても別のモデルへ自動では替えない"`
	Status   string `json:"status" jsonschema:"assignment_usable（割り当てを使える）/ condition_match（割り当てが使えず、条件に合う利用可能なモデルで生成する）/ blocked（使えるモデルが無く、生成は断られる）"`
	// Assigned lists the primary and the alternative of the assignment (one entry when they are the same model).
	Assigned []AssignedModel `json:"assigned"`
	// Use is the model a generation would run on first: the first usable one of the assignment, or the chosen one; absent while blocked.
	Use *Candidate `json:"use,omitempty" jsonschema:"いま生成を始めたら最初に使うモデルと経路・費用の基準（blocked のときは無い）"`
	// Selection and Reason say how Use was chosen: "assignment" or "condition_match" (a match of conditions, never a quality ranking).
	Selection modelreg.Selection `json:"selection,omitempty" jsonschema:"assignment（割り当てどおり）/ condition_match（条件に合うため選んだ。品質の優劣で選んだのではない）"`
	Reason    string             `json:"reason,omitempty" jsonschema:"選んだ理由"`
	// Candidates are the usable models that match the conditions (the suggestions of a blocked explicit assignment), in the order of the selection rule.
	Candidates []Candidate `json:"candidates,omitempty" jsonschema:"条件に合って今使えるモデル（選ぶ規則の順。blocked のときの提案）"`
	Message    string      `json:"message,omitempty" jsonschema:"blocked の理由と直し方（日本語）"`
}

// Report is the RouteReport of every image purpose and every Quality Policy of the assignment, images first. The conditions that belong to one Shot (its length) are not
// known here: a video route is judged for a Shot that has a Keyframe, by the Quality Policy's resolution alone.
func (e Env) Report() []RouteReport {
	e = e.memoised()
	var out []RouteReport
	for _, p := range modeladapter.ImagePurposes() {
		r := e.Config.ImageRouteFor(p)
		pick, err := e.Image(p)
		out = append(out, e.report("image", string(p), "", e.explicitImage(p), pairOf(r.Primary.Model, r.Fallback.Model), pick.Route.Primary.Model, pick.Selected, err))
	}
	for _, q := range domain.QualityPolicies() {
		r := e.Config.Policies[q]
		pick, err := e.Video(q, VideoNeed{Keyframe: true})
		out = append(out, e.report("video", string(q), pick.Route.Primary.Resolution, e.explicitVideo(q), pairOf(r.Primary.Model, r.Fallback.Model), pick.Route.Primary.Model, pick.Selected, err))
	}
	return out
}

// report builds the entry of one route. first is the model the pick runs first; for a usable assignment it is the primary, which the Adapter skips for the
// alternative when the primary cannot be called, so the first usable of the assignment is named instead.
func (e Env) report(media, key, res string, explicit bool, assigned []domain.ModelName, first domain.ModelName, sel Selected, err error) RouteReport {
	r := RouteReport{Media: media, Key: key, Explicit: explicit}
	roles := []string{"primary", "fallback"}
	for i, m := range assigned {
		av := e.check(m)
		role := roles[min(i, 1)]
		r.Assigned = append(r.Assigned, AssignedModel{
			Role: role, Model: string(m), RouteKind: modelreg.RouteOf(string(m)), Available: av.Available, UnavailableMessage: av.Message, SettingsRoute: av.Route,
		})
	}
	if err != nil {
		r.Status, r.Message = StatusBlocked, err.Error()
		if nu := new(NoUsableError); errors.As(err, &nu) {
			r.Candidates, r.Message = nu.Candidates, nu.Cause()
		}
		return r
	}
	r.Selection, r.Reason = sel.Selection, sel.Reason
	if sel.Substituted() {
		r.Status = StatusCondition
	} else {
		r.Status = StatusAssignment
		first = e.FirstUsable(first, assigned[len(assigned)-1])
	}
	r.Use = &Candidate{Model: first, Route: modelreg.RouteOf(string(first)), Basis: BasisOf(e.Config, first, media), Resolution: res}
	return r
}

// FirstUsable is the model the Adapter calls first for a route: the primary, or the alternative when the primary cannot be used (the Adapter skips it by
// itself). When neither can be used it is the primary.
func (e Env) FirstUsable(primary, fallback domain.ModelName) domain.ModelName {
	if !e.check(primary).Available && e.check(fallback).Available {
		return fallback
	}
	return primary
}

// Named is the model a progress card names for a route, with its route and why it was chosen: the model FirstUsable says, not the assignment's primary
// when the Adapter will run the alternative (fellBack is then true).
func (e Env) Named(sel Selected, primary, fallback domain.ModelName, media string) string {
	m := e.FirstUsable(primary, fallback)
	return string(m) + "（" + sel.Use(e.Config, m, media, m != primary).Label() + "）"
}
