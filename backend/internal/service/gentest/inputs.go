package gentest

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/inputcheck"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// The check of the input images against the chosen models is package inputcheck (#1009; spec/architecture/gentest/gentest.md §5.1). estimate runs it (so the
// screen's estimate and the MCP tools make the same check) and CheckInputs runs it for the form's notice.

func rolesOf(r Request) []string {
	roles := make([]string, len(r.References))
	for i, ref := range r.References {
		roles[i] = ref.Role
	}
	return roles
}

// modelOf is the model as the check reads it.
func modelOf(info modelcatalog.Info) inputcheck.Model {
	m := inputcheck.Model{ID: info.ID, Label: info.Label, Video: info.Media == gentestrepo.MediaVideo, Requires: info.RequiresInputs, TakesImages: info.AcceptsReferences}
	if v := info.Video; v != nil {
		m.TakesImages = v.FirstFrame || v.LastFrame || v.MaxReferenceImages > 0
	}
	return m
}

// checkInputs is the stop of the check: the first model that lacks a required input, else (when images are attached) that no chosen model uses any of
// them. Nil when the request may be sent (with warnings, which the plans carry).
func checkInputs(r Request, plans []modelPlan) *apperr.Error {
	roles, models, uses := rolesOf(r), make([]inputcheck.Model, len(plans)), make([]inputcheck.Plan, len(plans))
	for i, p := range plans {
		models[i], uses[i] = modelOf(p.info), inputcheck.Plan{Used: p.used, Unused: p.unused}
		if e := inputcheck.Missing(r.Media == gentestrepo.MediaVideo, roles, models[i]); e != nil {
			return e
		}
	}
	return inputcheck.AllUnused(roles, models, uses)
}

// AboutInputs says the warning is about an input image the model leaves out (the confirmation shows these in a box of their own).
func (w Warning) AboutInputs() bool {
	return slices.Contains([]string{WarnInputsDropped, WarnTooManyReference, WarnFramesOverFree}, w.Code)
}

// InputCheck is how the chosen models take the attached input images: the stop (nil when the request may be sent) and the warnings.
type InputCheck struct {
	Block    *apperr.Error
	Warnings []Warning
}

// CheckInputs checks the input images of a request that may be unfinished (the form being filled in: no prompt yet, uploads not staged) against its
// models: the same check as the estimate's, without looking at the images. Without a model that can be called nothing is checked.
func (s *Service) CheckInputs(r Request) InputCheck {
	models, err := s.chosenModels(r)
	if err != nil {
		return InputCheck{}
	}
	var out InputCheck
	plans := make([]modelPlan, len(models))
	for i, m := range models {
		plans[i] = modelPlan{info: m}
		if r.Media == gentestrepo.MediaImage {
			s.planImage(r, &plans[i])
		} else {
			caps, _ := s.Adapter.Config().VideoCaps(domain.ModelName(m.ID))
			plans[i].applyInputs(inputcheck.Video(caps, m.Label, rolesOf(r)))
		}
		for _, w := range plans[i].warnings {
			if w.AboutInputs() {
				out.Warnings = append(out.Warnings, w)
			}
		}
	}
	out.Block = checkInputs(r, plans)
	return out
}

// applyInputs puts the decision of inputcheck into the plan: the images the model is given and not, and a warning for each kind it leaves out.
func (p *modelPlan) applyInputs(in inputcheck.Plan) {
	p.used, p.unused = in.Used, in.Unused
	for _, n := range in.Notes {
		p.warnings = append(p.warnings, Warning{Model: p.info.ID, Code: n.Code, Message: n.Message})
	}
}

// DefaultInputRole is the role a picture that is added to the form starts with: the first frame while a chosen model needs one (an I2V model) and
// no picture has that role yet, else the whole picture ("reference"). The role stays visible in the form's select; nothing is rewritten silently.
func (s *Service) DefaultInputRole(media string, modelIDs []string, existing []RefInput) string {
	if media != gentestrepo.MediaVideo || slices.ContainsFunc(existing, func(x RefInput) bool { return x.Role == gentestrepo.RoleFirstFrame }) {
		return gentestrepo.RoleReference
	}
	have := s.catalog.Models(media)
	for _, id := range modelIDs {
		if i := slices.IndexFunc(have, func(m modelcatalog.Info) bool { return m.ID == id }); i >= 0 && have[i].Available && slices.Contains(have[i].RequiresInputs, gentestrepo.RoleFirstFrame) {
			return gentestrepo.RoleFirstFrame
		}
	}
	return gentestrepo.RoleReference
}
