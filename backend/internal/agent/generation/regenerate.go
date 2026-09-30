package generation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/promptplanner"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Progress vocabulary of the Repair Agent, which reports on the same WebSocket (/ws/shots/:id).
const (
	StageRepairing Stage  = "repairing" // the Repair Agent is making a new Take
	ActionRepair   Action = "repair"    // a job of the repair kind
)

var (
	// ErrNoSpec means the Shot has no Canonical Shot Spec to repair.
	ErrNoSpec = errors.New("generation: the shot has no Canonical Shot Spec")
	// ErrNoOtherModel means Different Model has no other video model to go to: the Video Model of the
	// Shot is fixed by the user, or the Quality Policy routes to a single model.
	ErrNoOtherModel = errors.New("generation: there is no other video model to use")
)

// Regeneration is one repair of a Take by the Repair Agent (spec/spec.md §13): what failed, what the
// Judge and the Repair Agent instruct, and how the new Take is to be made.
type Regeneration struct {
	// Action is what to do; it MUST be an action that regenerates (RepairAction.Regenerates).
	Action domain.RepairAction
	// Issues are the Issues of the Judge result being repaired.
	Issues []domain.JudgeIssue
	// Instructions are snake_case repair instructions ("reduce_camera_speed_40_percent").
	Instructions []string
	// Preserve is what the Judge found good and must be kept ("character", "acting", ...).
	Preserve []string
	// SourceModel is the model of the Take that is repaired: Different Model goes to another one.
	SourceModel domain.ModelName
	// Ranges are the time ranges of the Take that are being fixed. They are set for a Partial
	// Regeneration whose new Take is spliced into the repaired one: only these ranges of the new Take
	// are used, so the Prompt Planner is told to change little else.
	Ranges []domain.TimeRange
	// Direction is a free-text direction of the director (English), for a regeneration the director
	// asked for in DIRECTOR MODE (there are no Issues then).
	Direction string
}

// RegenerationStrategy plans the single Take of a repair: the strategy of the Quality Policy, with
// the models swapped for Different Model. It fails with ErrBudgetExceeded when not even that one
// Take fits the budget, and with ErrNoOtherModel when Different Model has nowhere to go. CostPerTake
// is the estimate the Retry Policy weighs (Max cost per Shot).
func (o *Orchestrator) RegenerationStrategy(ctx context.Context, ref repository.ShotRef, r Regeneration) (Strategy, error) {
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return Strategy{}, err
	}
	budget, err := o.gens.Budget(ctx, ref.ProjectID, ref.Shot.ID)
	if err != nil {
		return Strategy{}, err
	}
	locks, err := o.gens.ShotLocks(ctx, ref.Shot.ID)
	if err != nil {
		return Strategy{}, err
	}
	cfg := o.adapter.Config()
	s, err := DecideWithModel(cfg, policy.Quality, ref.Shot.Duration, budget, FixedModel(locks))
	if err != nil {
		return Strategy{}, err
	}
	s.Takes = 1
	if r.Action != domain.RepairDifferentModel {
		return s, nil
	}
	if FixedModel(locks) != "" {
		return Strategy{}, ErrNoOtherModel
	}
	// Different Model: the automatic fallback between the models of the Quality Policy (model-adapters.md §4).
	primary, fallback := s.Primary, s.Fallback
	if primary.Model == r.SourceModel {
		primary, fallback = fallback, primary
	}
	if primary.Model == r.SourceModel {
		return Strategy{}, ErrNoOtherModel
	}
	caps, ok := modeladapter.Capabilities(primary.Model)
	if !ok {
		return Strategy{}, ErrNoOtherModel
	}
	s.Primary, s.Fallback = primary, fallback
	s.DurationSeconds = caps.SnapDuration(ref.Shot.Duration)
	s.CostPerTake = cfg.VideoCost(primary, s.DurationSeconds)
	if affordable(remainingBudget(budget), s.CostPerTake) == 0 {
		return Strategy{}, ErrBudgetExceeded
	}
	s.Reasons = append(s.Reasons, fmt.Sprintf("different model: %s instead of %s", primary.Model, r.SourceModel))
	return s, nil
}

// Regenerate makes the new Take of a repair and has it judged. The Take is a new row; the Take that
// is repaired is not touched. It returns the ID of the new Take.
//
//   - Prompt Repair: the Prompt Planner revises the Shot Spec for the Issues; LOCKed parts stay as they are.
//   - Partial Regeneration: the same, and the parts the Judge asked to preserve stay as they are too. The
//     new Take covers the whole Shot (video models make whole clips); splicing only the problem time
//     range of it into the repaired Take is the caller's job (repair.Agent, with Splicer).
//   - Full Retake: the same Shot Spec, a new generation.
//   - Different Model: the same Shot Spec for the other video model of the Quality Policy.
//   - Reference Reinforcement: the constraints that protect what the References show are added to the
//     Shot Spec, the References of the failed kind go first, and a model that takes free references
//     uses them instead of the Keyframe as its first frame.
func (o *Orchestrator) Regenerate(ctx context.Context, ref repository.ShotRef, r Regeneration) (string, error) {
	if !r.Action.Regenerates() {
		return "", fmt.Errorf("generation: %q does not regenerate", r.Action)
	}
	shot := ref.Shot
	current, ok := shot.CanonicalSpec()
	if !ok {
		return "", ErrNoSpec
	}
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return "", err
	}
	locks, err := o.gens.ShotLocks(ctx, shot.ID)
	if err != nil {
		return "", err
	}
	strategy, err := o.RegenerationStrategy(ctx, ref, r)
	if err != nil {
		return "", err
	}

	spec := current
	references := shot.References()
	switch r.Action {
	case domain.RepairPrompt, domain.RepairPartialRegeneration:
		if spec, err = o.reviseSpec(ctx, ref, policy, locks, current, r); err != nil {
			return "", err
		}
	case domain.RepairReferenceReinforce:
		boost := boostedKinds(r)
		if reinforced := reinforce(current, boost); len(domain.LockedChanges(locks, current, reinforced)) == 0 {
			spec = reinforced
		}
		references = reinforceReferences(references, boost, strategy.Primary.Model)
	}

	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return "", err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID)
	if err != nil {
		return "", err
	}
	job, err := o.videoJob(ctx, ref.ProjectID, spec, brief, strategy.DurationSeconds, references)
	if err != nil {
		return "", err
	}
	record, err := recordOf(strategy, job, "")
	if err != nil {
		return "", err
	}
	pv, err := o.gens.SavePromptedSpec(ctx, shot.ID, spec, record)
	if err != nil {
		return "", err
	}
	title := fmt.Sprintf("Shot %d · %s", shot.OrderIndex+1, r.Action.Label())
	takeID, err := o.makeTake(ctx, ref, pv, strategy, job, domain.TakeLineage{WorldStateVersion: world.Version, References: references}, title, policy.Effective().MaxGenerationTime, "")
	if err != nil {
		return "", err
	}
	o.judgeTake(ctx, takeID)
	if r.Action == domain.RepairPrompt || r.Action == domain.RepairPartialRegeneration {
		o.updateWorldState(ctx, ref, spec, takeID) // the Shot Spec changed, and so may the state it leaves
	}
	return takeID, nil
}

// reviseSpec has the Prompt Planner revise the spec for the Issues of the failed Take.
func (o *Orchestrator) reviseSpec(ctx context.Context, ref repository.ShotRef, policy domain.ProjectPolicy, locks domain.ParameterLocks, current domain.ShotSpec, r Regeneration) (domain.ShotSpec, error) {
	if locks.Locked(domain.ParamPrompt) {
		return domain.ShotSpec{}, &repository.LockedParametersError{Parameters: []domain.LockParameter{domain.ParamPrompt}}
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return domain.ShotSpec{}, err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID)
	if err != nil {
		return domain.ShotSpec{}, err
	}
	shots, err := o.gens.ListProjectShots(ctx, ref.ProjectID)
	if err != nil {
		return domain.ShotSpec{}, err
	}
	fixed := locks.LockedParts()
	if r.Action == domain.RepairPartialRegeneration {
		fixed = slices.Concat(fixed, preservedParts(r.Preserve))
	}
	return o.planner.Plan(ctx, promptplanner.Input{
		Brief: brief, Template: policy.Template, Scene: ref.Scene, Shot: ref.Shot,
		Instruction: repairInstruction(r), World: world, Previous: previousSpec(shots, ref.Shot.ID), References: ref.Shot.References(),
		Current: &current, Fixed: fixed, Manual: locks.PartsWith(domain.LockManual),
	})
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
// when it accepts fewer). A model that takes free reference images gets no Keyframe as first frame:
// it reads either the frames or the references, and the references are what is being reinforced.
func reinforceReferences(refs []domain.Reference, boost []domain.ReferenceKind, model domain.ModelName) []domain.Reference {
	out := make([]domain.Reference, 0, len(refs))
	dropKeyframe := false
	if caps, ok := modeladapter.Capabilities(model); ok && caps.MaxReferenceImages > 0 {
		dropKeyframe = slices.ContainsFunc(refs, func(r domain.Reference) bool { return r.Kind != domain.RefKeyframe })
	}
	for _, r := range refs {
		if dropKeyframe && r.Kind == domain.RefKeyframe {
			continue
		}
		out = append(out, r)
	}
	rank := func(k domain.ReferenceKind) int {
		if i := slices.Index(boost, k); i >= 0 {
			return i
		}
		return len(boost)
	}
	slices.SortStableFunc(out, func(a, b domain.Reference) int { return rank(a.Kind) - rank(b.Kind) })
	return out
}
