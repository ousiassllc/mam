package generation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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
	instruction := repairInstruction(r)
	notes, err := o.shotNotes(ctx, ref, instruction)
	if err != nil {
		return domain.ShotSpec{}, nil, err
	}
	fixed := locks.LockedParts()
	if r.Action == domain.RepairPartialRegeneration {
		fixed = slices.Concat(fixed, preservedParts(r.Preserve))
	}
	spec, err := o.planner.Plan(ctx, promptplanner.Input{
		Brief: brief, Template: policy.Template, Scene: ref.Scene, Shot: ref.Shot,
		Instruction: instruction, World: world, Previous: previousSpec(shots, ref.Shot.ID), References: ref.Shot.References(),
		Current: &current, Fixed: fixed, Manual: locks.PartsWith(domain.LockManual), Notes: notes, Brand: brand,
	})
	return spec, domain.NoteRefs(notes), err
}

// repairInstruction is the direction the Prompt Planner receives: the Issues in their structured
// form and the repair instructions, in English.
func repairInstruction(r Regeneration) string {
	var b strings.Builder
	if len(r.Issues) == 0 {
		b.WriteString("The director wants a new take of this shot with a change.")
	} else {
		b.WriteString("The previous take of this shot failed the video judge. Revise the spec so the next take fixes these issues:")
	}
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "\n- %s (%s)", is.Type, is.Severity)
		if is.TimeRange != "" {
			fmt.Fprintf(&b, " at %s", is.TimeRange)
		}
		if is.Description != "" {
			fmt.Fprintf(&b, ": %s", is.Description)
		}
	}
	if r.Direction != "" {
		b.WriteString("\nDirector's instruction: " + r.Direction)
	}
	if len(r.Ranges) > 0 {
		texts := make([]string, len(r.Ranges))
		for i, tr := range r.Ranges {
			texts[i] = fmt.Sprintf("%.1f-%.1fs", tr.From, tr.To)
		}
		b.WriteString("\nOnly " + strings.Join(texts, ", ") + " of the new take will be used, spliced into the previous take: change only what happens there and keep everything else as it was.")
	}
	if len(r.Instructions) > 0 {
		b.WriteString("\nRepair instructions: " + strings.Join(r.Instructions, ", ") + ".")
	}
	if len(r.Preserve) > 0 {
		b.WriteString("\nKeep what was good: " + strings.Join(r.Preserve, ", ") + ".")
	}
	return b.String()
}

// preservedParts maps what the Judge found good ("character", "background", ...) to the parts of
// the Shot Spec that then stay fixed. Unknown words are ignored.
func preservedParts(preserve []string) []domain.SpecPart {
	var parts []domain.SpecPart
	for _, p := range preserve {
		var part domain.SpecPart
		switch p {
		case "character", "acting", "camera", "lighting", "product":
			part = domain.SpecPart(p)
		case "background", "environment", "location":
			part = domain.PartLocation
		default:
			continue
		}
		if !slices.Contains(parts, part) {
			parts = append(parts, part)
		}
	}
	return parts
}

// boostedKinds are the Reference kinds Reference Reinforcement strengthens: those of the Issues that
// failed (a drifting face → Character, a deformed logo → Product, ...) and of the instructions.
func boostedKinds(r Regeneration) []domain.ReferenceKind {
	var kinds []domain.ReferenceKind
	add := func(k domain.ReferenceKind) {
		if !slices.Contains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	for _, is := range r.Issues {
		switch {
		case is.Type == domain.IssueTypeFaceIdentityDrift, strings.Contains(is.Type, "face"), strings.Contains(is.Type, "character"),
			is.Dimension == domain.DimCharacterIdentity, is.Dimension == domain.DimCharacterConsistency, is.Dimension == domain.DimFace:
			add(domain.RefCharacter)
		case strings.Contains(is.Type, "product"), strings.Contains(is.Type, "logo"), strings.Contains(is.Type, "label"),
			is.Dimension == domain.DimProductAccuracy, is.Dimension == domain.DimProductGeometry, is.Dimension == domain.DimLogoTextAccuracy:
			add(domain.RefProduct)
		case is.Dimension == domain.DimBackground, is.Dimension == domain.DimSceneContinuity:
			add(domain.RefLocation)
		case is.Dimension == domain.DimLighting:
			add(domain.RefLighting)
		}
	}
	for _, in := range r.Instructions {
		switch {
		case strings.Contains(in, "face"), strings.Contains(in, "character"):
			add(domain.RefCharacter)
		case strings.Contains(in, "product"), strings.Contains(in, "logo"):
			add(domain.RefProduct)
		}
	}
	if len(kinds) == 0 {
		add(domain.RefCharacter)
		add(domain.RefProduct)
	}
	return kinds
}

// reinforce adds the constraints that make the model keep what the boosted References show.
func reinforce(spec domain.ShotSpec, boost []domain.ReferenceKind) domain.ShotSpec {
	spec.Constraints = slices.Clone(spec.Constraints)
	add := func(c string) {
		if !slices.Contains(spec.Constraints, c) {
			spec.Constraints = append(spec.Constraints, c)
		}
	}
	for _, k := range boost {
		switch k {
		case domain.RefCharacter:
			if spec.Character != nil {
				add("preserve_face_identity")
			}
		case domain.RefProduct:
			if spec.Product != nil {
				add("preserve_product_geometry")
				add("preserve_logo")
			}
		case domain.RefLocation:
			add("preserve_environment_geometry")
		}
	}
	return spec
}

// reinforceReferences puts the References of the boosted kinds first (the model keeps the first ones
// when it accepts fewer). Whether the Keyframe is left out is decided per model in modeladapter.Prepare
// (VideoJob.PreferReferences), so a fail over model keeps the input it can use.
func reinforceReferences(refs []domain.Reference, boost []domain.ReferenceKind) []domain.Reference {
	out := slices.Clone(refs)
	rank := func(k domain.ReferenceKind) int {
		if i := slices.Index(boost, k); i >= 0 {
			return i
		}
		return len(boost)
	}
	slices.SortStableFunc(out, func(a, b domain.Reference) int { return rank(a.Kind) - rank(b.Kind) })
	return out
}
