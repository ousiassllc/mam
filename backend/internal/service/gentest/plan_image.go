package gentest

import (
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
)

// planImage decides which references, which LoRA and which pose option an image model takes of the request (refs.Decide).
func (s *Service) planImage(r Request, p *modelPlan, warn func(code, format string, args ...any)) {
	roles := make([]string, len(r.References))
	for i, ref := range r.References {
		roles[i] = ref.Role
	}
	info := p.info
	d := refs.Decide(refs.Model{
		Label: info.Label, AcceptsReferences: info.AcceptsReferences, MaxReferences: info.MaxReferenceImages, Roles: info.ReferenceRoles,
		SupportsLoRA: info.SupportsLoRA,
	}, roles, r.LoRA != nil, r.AvoidReferencePose)
	p.used, p.lora, p.avoidPose = d.Used, d.LoRA, d.AvoidPose
	for _, w := range d.Warnings {
		warn(w.Code, "%s", w.Message)
	}
}
