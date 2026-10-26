package generation

import (
	"context"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Start begins generating the Shot: it claims the Shot (ErrShotBusy when it is already being
// generated), checks that at least one Take fits the budget (ErrBudgetExceeded) and queues the job.
// instruction is an optional extra direction ("もう少しカメラを寄せて").
func (o *Orchestrator) Start(ctx context.Context, shotID, instruction string) (domain.Job, error) {
	return o.StartDirected(ctx, shotID, instruction, nil)
}

// StartDirected is Start with the parts of the Shot Spec the director asked to preserve (the
// LOCKed parts are always preserved).
func (o *Orchestrator) StartDirected(ctx context.Context, shotID, instruction string, preserve []domain.SpecPart) (domain.Job, error) {
	return o.start(ctx, shotID, ActionGenerate, instruction, preserve)
}

// StartCandidates begins Prompt Candidate Generation for the Shot (F-08): the Prompt Planner writes
// candidate specs with different shooting strategies, each is generated, and the AI Video Judge
// picks the Best Take, all according to the Generation Mode of the Quality Policy (see
// generateCandidates). It fails like Start, and with ErrJudgeUnavailable when the Judge is off.
func (o *Orchestrator) StartCandidates(ctx context.Context, shotID, instruction string) (domain.Job, error) {
	if o.judge == nil {
		return domain.Job{}, ErrJudgeUnavailable
	}
	return o.start(ctx, shotID, ActionCandidates, instruction, nil)
}

// UsesCandidates reports whether the generation the Quality Policy calls for is Prompt Candidate
// Generation for the Shot. Every Generation Mode of spec/spec.md §8 (Quality / Balanced / Economy, from
// MAX QUALITY / BALANCED / FAST) generates through Candidates and the Judge, so it is the case unless
// the Judge is off (nothing could pick the Best Take) or the Prompt is LOCKed (Candidates rewrite the
// spec, so StartCandidates refuses with a 423 and the Shot is made from the spec it has).
func (o *Orchestrator) UsesCandidates(ctx context.Context, shot domain.Shot) (bool, error) {
	if o.judge == nil {
		return false, nil
	}
	if _, hasSpec := shot.CanonicalSpec(); !hasSpec {
		return true, nil
	}
	locks, err := o.gens.ShotLocks(ctx, shot.ID)
	if err != nil {
		return false, err
	}
	return !locks.Locked(domain.ParamPrompt), nil
}

// StartByPolicy begins generating the Shot the way the Quality Policy of its Project calls for: Prompt
// Candidate Generation (StartCandidates) when UsesCandidates, else the single generation (StartDirected).
// It is the one entry the generation the system starts for the user goes through (the Shot card's
// 生成, Continue, Branch, the editor's reshoot, the director's regenerate), so that MAX QUALITY is
// honoured everywhere. preserve is as for StartDirected. The explicit POST /shots/:id/generate is the
// single generation (Start).
func (o *Orchestrator) StartByPolicy(ctx context.Context, shotID, instruction string, preserve []domain.SpecPart) (domain.Job, error) {
	ref, err := o.gens.GetShot(ctx, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	byCandidates, err := o.UsesCandidates(ctx, ref.Shot)
	if err != nil {
		return domain.Job{}, err
	}
	if byCandidates {
		return o.start(ctx, shotID, ActionCandidates, instruction, preserve)
	}
	return o.StartDirected(ctx, shotID, instruction, preserve)
}

func (o *Orchestrator) start(ctx context.Context, shotID string, action Action, instruction string, preserve []domain.SpecPart) (domain.Job, error) {
	ref, err := o.gens.GetShot(ctx, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	instruction = strings.TrimSpace(instruction)
	// A direction that contains a forbidden expression of a LOCK Brand constraint is refused up front.
	// Advisory constraints are not an error. The spec that will be generated is checked the same way,
	// before the job is queued, so a violation is HTTP 403 and creates neither a Take nor a progress job.
	brand, err := o.brandConstraints(ctx, ref.ProjectID)
	if err != nil {
		return domain.Job{}, err
	}
	if err := brand.CheckText(instruction); err != nil {
		return domain.Job{}, err
	}
	// Candidates always rewrite the spec, so a LOCKed Prompt rules them out even without an instruction.
	if _, hasSpec := ref.Shot.CanonicalSpec(); hasSpec && (instruction != "" || action == ActionCandidates) {
		locks, err := o.gens.ShotLocks(ctx, shotID)
		if err != nil {
			return domain.Job{}, err
		}
		if locks.Locked(domain.ParamPrompt) {
			return domain.Job{}, &repository.LockedParametersError{Parameters: []domain.LockParameter{domain.ParamPrompt}}
		}
	}
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return domain.Job{}, err
	}
	budget, err := o.gens.Budget(ctx, ref.ProjectID, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	strategy, err := o.decide(ctx, ref, policy, specOf(ref.Shot), budget)
	if err != nil {
		return domain.Job{}, err
	}
	p := payload{Action: action, ShotID: shotID, Instruction: instruction, Preserve: preserve}
	switch action {
	case ActionCandidates:
		p.CandidateSpecs, err = o.prepareCandidates(ctx, ref, instruction, preserve, strategy)
	default:
		p.Spec, p.NoteRefs, err = o.prepareGenerate(ctx, ref, instruction, preserve)
	}
	if err != nil {
		return domain.Job{}, err
	}
	job, err := o.enqueue(ctx, ref, p)
	if err != nil {
		return domain.Job{}, err
	}
	if action == ActionCandidates {
		strategy.Takes = plannedTakes(domain.ModeOf(policy.Quality), policy.Effective(), strategy)
	}
	o.warnLowDisk(ctx, ref, strategy)
	return job, nil
}

// decide is the generation strategy of the Shot: the Quality Policy, Multi-model routing (the nature
// of the Shot, the Judge record of the models, cost, and the state of the vendors), cost and model
// capabilities, and the Video Model when the user fixed one (Video Model MANUAL or LOCK). spec is the
// Shot Spec the Shot is generated from (nil when it has none yet).
func (o *Orchestrator) decide(ctx context.Context, ref repository.ShotRef, policy domain.ProjectPolicy, spec *domain.ShotSpec, budget repository.Budget) (Strategy, error) {
	locks, err := o.gens.ShotLocks(ctx, ref.Shot.ID)
	if err != nil {
		return Strategy{}, err
	}
	cfg, forced := o.adapter.Config(), FixedModel(locks)
	var in *RoutingInput
	if forced == "" {
		in = &RoutingInput{}
		if cfg.Routing.Enabled {
			records, err := o.gens.ModelRecords(ctx, ref.ProjectID)
			if err != nil {
				return Strategy{}, err
			}
			in = &RoutingInput{
				Spec: spec, References: ref.Shot.References(), Records: records,
				MinAcceptance: policy.Effective().MinAcceptanceScore, Unavailable: o.adapter.Unavailable,
			}
		}
	}
	s, err := DecideRouted(cfg, policy.Quality, ref.Shot.Duration, budget, forced, in)
	if err == nil { // an adult model while the age confirmation is off: refused whole, never replaced by the alternative
		err = o.adapter.Gate.Check(s.Primary.Model, s.Fallback.Model)
	}
	return s, err
}

// specOf is the Canonical Shot Spec of the Shot, or nil when it has none.
func specOf(shot domain.Shot) *domain.ShotSpec {
	if spec, ok := shot.CanonicalSpec(); ok {
		return &spec
	}
	return nil
}

// StartReferencePack begins building (or rebuilding) the Reference Pack of the Shot. It fails with
// ErrBudgetExceeded when not even one image fits the budget ceiling of the Project or the Shot (F-22):
// how many images the Art Director plans is only known while the job runs, so the cheapest image
// model is the least the pack costs.
func (o *Orchestrator) StartReferencePack(ctx context.Context, shotID string) (domain.Job, error) {
	ref, err := o.gens.GetShot(ctx, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	if err := o.checkPackModels(); err != nil {
		return domain.Job{}, err
	}
	budget, err := o.gens.Budget(ctx, ref.ProjectID, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	if affordable(budget.Remaining(), o.cheapestImageCost()) == 0 {
		return domain.Job{}, ErrBudgetExceeded
	}
	return o.enqueue(ctx, ref, payload{Action: ActionReferencePack, ShotID: shotID})
}

// cheapestImageCost is the estimated cost of one image of the cheapest priced image model (0 when
// none is priced).
func (o *Orchestrator) cheapestImageCost() float64 {
	cfg, cheapest := o.adapter.Config(), 0.0
	for _, m := range domain.ImageModels() {
		if c := cfg.ImageCost(m); c > 0 && (cheapest == 0 || c < cheapest) {
			cheapest = c
		}
	}
	return cheapest
}

// refuseBusy is ErrShotBusy for a Shot that is being generated or repaired. enqueue's claim stays
// the authority on who owns the Shot; this check lets the callers refuse before they pay for the
// Prompt Planner a spec that would be thrown away.
func refuseBusy(ref repository.ShotRef) error {
	if ref.Shot.State.Busy() {
		return ErrShotBusy
	}
	return nil
}
