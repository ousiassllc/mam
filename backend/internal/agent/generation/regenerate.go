package generation

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
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
	// Prepared is a Shot Spec already checked against LOCK brand constraints. When set, Regenerate
	// does not ask the Prompt Planner again. NoteRefs are the notes that spec was planned from.
	Prepared *domain.ShotSpec
	NoteRefs []domain.NoteRef
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
	s, err := o.decide(ctx, ref, policy, specOf(ref.Shot), budget)
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
	if affordable(budget.Remaining(), s.CostPerTake) == 0 {
		return Strategy{}, ErrBudgetExceeded
	}
	reason := fmt.Sprintf("different model: %s instead of %s", primary.Model, r.SourceModel)
	s.Reasons = append(s.Reasons, reason)
	if s.Routing.Mode != "" {
		s.Routing.Primary = domain.RoutingModel{Model: primary.Model, Resolution: primary.Resolution}
		s.Routing.Fallback = domain.RoutingModel{Model: fallback.Model, Resolution: fallback.Resolution}
		s.Routing.Reason += "; " + reason
	}
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
	brand, err := o.brandConstraints(ctx, ref.ProjectID)
	if err != nil {
		return "", err
	}
	if err := brand.CheckText(r.Direction); err != nil {
		return "", err
	}
	strategy, err := o.RegenerationStrategy(ctx, ref, r)
	if err != nil {
		return "", err
	}

	spec := current
	references := shot.References()
	var noteRefs []domain.NoteRef // the notes the Planner was given; none when it did not run
	switch {
	case r.Prepared != nil:
		spec, noteRefs = *r.Prepared, r.NoteRefs
	case r.Action == domain.RepairPrompt || r.Action == domain.RepairPartialRegeneration:
		if spec, noteRefs, err = o.reviseSpec(ctx, ref, policy, locks, brand, current, r); err != nil {
			return "", err
		}
	case r.Action == domain.RepairReferenceReinforce:
		boost := boostedKinds(r)
		if reinforced := reinforce(current, boost); len(domain.LockedChanges(locks, current, reinforced)) == 0 {
			spec = reinforced
		}
		references = reinforceReferences(references, boost)
	}

	if err := brand.CheckText(spec.BrandText()); err != nil {
		return "", err
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return "", err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return "", err
	}
	job, err := o.videoJob(ctx, ref.ProjectID, spec, brief, strategy.DurationSeconds, references)
	if err != nil {
		return "", err
	}
	job.PreferReferences = r.Action == domain.RepairReferenceReinforce
	record, err := recordOf(strategy, job, "")
	if err != nil {
		return "", err
	}
	pv, err := o.gens.SavePromptedSpec(ctx, shot.ID, spec, record, noteRefs)
	if err != nil {
		return "", err
	}
	o.warnLowDisk(ctx, ref, strategy) // the repair's Take is made without start(), which warns for the other paths
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
