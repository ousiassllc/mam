package generation

import (
	"context"
	"errors"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/generation/repairspec"
	"github.com/ousiassllc/mam/backend/internal/agent/promptplanner"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// PreparePartial revises the Shot Spec for a director's Partial Regeneration and checks the direction
// and the spec against LOCK brand constraints. checked is false when the spec was not prepared (there
// is nothing to revise, or the planner failed for a reason other than a brand violation): the job
// then plans it and reports that failure. A brand violation is returned so nothing is queued.
func (o *Orchestrator) PreparePartial(ctx context.Context, ref repository.ShotRef, r Regeneration) (spec domain.ShotSpec, notes []domain.NoteRef, checked bool, err error) {
	brand, err := o.brandConstraints(ctx, ref.ProjectID)
	if err != nil {
		return domain.ShotSpec{}, nil, false, err
	}
	if err := brand.CheckText(r.Direction); err != nil {
		return domain.ShotSpec{}, nil, false, err
	}
	current, ok := ref.Shot.CanonicalSpec()
	if !ok {
		return domain.ShotSpec{}, nil, false, nil
	}
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return domain.ShotSpec{}, nil, false, err
	}
	locks, err := o.gens.ShotLocks(ctx, ref.Shot.ID)
	if err != nil {
		return domain.ShotSpec{}, nil, false, err
	}
	spec, notes, err = o.reviseSpec(ctx, ref, policy, locks, brand, current, r)
	if err != nil {
		if errors.Is(err, domain.ErrBrandViolation) {
			return domain.ShotSpec{}, nil, false, err
		}
		return domain.ShotSpec{}, nil, false, nil
	}
	return spec, notes, true, nil
}

// reviseSpec has the Prompt Planner revise the spec for the Issues of the failed Take. It also returns
// the versions of the production notes the Planner was given (for prompt_version.note_refs).
func (o *Orchestrator) reviseSpec(ctx context.Context, ref repository.ShotRef, policy domain.ProjectPolicy, locks domain.ParameterLocks, brand domain.BrandConstraints, current domain.ShotSpec, r Regeneration) (domain.ShotSpec, []domain.NoteRef, error) {
	if locks.Locked(domain.ParamPrompt) {
		return domain.ShotSpec{}, nil, &repository.LockedParametersError{Parameters: []domain.LockParameter{domain.ParamPrompt}}
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return domain.ShotSpec{}, nil, err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return domain.ShotSpec{}, nil, err
	}
	shots, err := o.gens.StoryShots(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return domain.ShotSpec{}, nil, err
	}
	instruction := repairspec.Instruction(r.repair())
	notes, err := o.shotNotes(ctx, ref, instruction)
	if err != nil {
		return domain.ShotSpec{}, nil, err
	}
	fixed := locks.LockedParts()
	if r.Action == domain.RepairPartialRegeneration {
		fixed = slices.Concat(fixed, repairspec.PreservedParts(r.Preserve))
	}
	spec, err := o.planner.Plan(ctx, promptplanner.Input{
		Brief: brief, Template: policy.Template, Scene: ref.Scene, Shot: ref.Shot,
		Instruction: instruction, World: world, Previous: previousSpec(shots, ref.Shot.ID), References: ref.Shot.References(),
		Current: &current, Fixed: fixed, Manual: locks.PartsWith(domain.LockManual), Notes: notes, Brand: brand,
	})
	return spec, domain.NoteRefs(notes), err
}
