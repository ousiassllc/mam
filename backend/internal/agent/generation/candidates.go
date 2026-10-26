package generation

import (
	"context"
	"errors"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrJudgeUnavailable means Prompt Candidate Generation cannot run because the AI Video Judge is off:
// without it there is no way to pick the Best Take (503).
var ErrJudgeUnavailable = errors.New("generation: the AI Video Judge is not available")

// plannedTakes is the most Takes Prompt Candidate Generation makes for a Shot in the mode:
//
//   - Quality Mode makes one Take per Candidate strategy at the same time, as many as the budget
//     of the strategy allows.
//   - Balanced Mode makes the first Candidate and adds one more per retry the Retry Policy allows,
//     up to one Take per strategy.
//   - Economy Mode makes the first Take and retakes it once per retry the Retry Policy allows.
//
// Balanced and Economy Mode spend one Take at a time and check the budget before each (mayRetry),
// so the count is an upper bound there.
func plannedTakes(mode domain.GenerationMode, eff domain.EffectivePolicy, s Strategy) int {
	candidates := len(domain.CandidateStrategies())
	n := 0
	switch mode {
	case domain.ModeBalanced:
		n = min(candidates, 1+eff.MaxRetries)
	case domain.ModeEconomy:
		n = 1 + eff.MaxRetries
	default:
		n = min(candidates, s.Takes)
	}
	return max(1, n)
}

// judged is a Take of Prompt Candidate Generation with the verdict of the Judge.
type judged struct {
	takeID   string
	number   int // 1-based order of the Take within the job
	strategy domain.CandidateStrategy
	spec     domain.ShotSpec
	// result is nil when the Take could not be judged.
	result *domain.JudgeResult
}

// severe reports whether the verdict is a serious problem: the Take failed on a LOCK violation or an
// Issue of high severity. Economy Mode retakes only after such a problem.
func severe(r domain.JudgeResult) bool {
	if r.Result != domain.JudgeFail {
		return false
	}
	for _, is := range r.Issues {
		if is.LockViolation || is.Severity == domain.SeverityHigh {
			return true
		}
	}
	return false
}

// better reports whether a is the better Take than b. It ranks like domain.PickTake, which the
// Judge uses for the AI Pick and for the Take AUTO MODE selects: a Take that passed beats one that
// failed, then the one with the higher mean of the four Take scores wins, then the earlier one. One
// order for both keeps the Selected Take, the AI Pick and the adopted Spec on the same Take. A Take
// that could not be judged is worse than any judged one.
func better(a, b judged) bool {
	switch {
	case a.result == nil || b.result == nil:
		return a.result != nil && b.result == nil
	case (a.result.Result == domain.JudgePass) != (b.result.Result == domain.JudgePass):
		return a.result.Result == domain.JudgePass
	}
	if sa, sb := domain.ScoreTake(a.result.Issues).Overall(), domain.ScoreTake(b.result.Issues).Overall(); sa != sb {
		return sa > sb
	}
	return a.number < b.number
}

// bestTake returns the Best Take among the judged ones. ok is false when none could be judged.
//
// accepted tells whether the Retry Policy accepts a judged Take (a pass that reaches the Minimum
// acceptance scores). Like TakeRepository.PickBest, which chooses the Take AUTO MODE selects, the Best
// Take is the best of the accepted Takes when there are any; with none, it is the best of all. A nil
// accepted counts every Take as accepted.
func bestTake(all []judged, accepted func(domain.JudgeResult) bool) (best judged, ok bool) {
	anyAccepted := false
	if accepted != nil {
		for _, j := range all {
			if j.result != nil && accepted(*j.result) {
				anyAccepted = true
				break
			}
		}
	}
	for _, j := range all {
		if j.result == nil || (anyAccepted && !accepted(*j.result)) {
			continue
		}
		if !ok || better(j, best) {
			best, ok = j, true
		}
	}
	return best, ok
}

// candidateRun is one Prompt Candidate Generation job.
type candidateRun struct {
	o           *Orchestrator
	ref         repository.ShotRef
	sc          shotContext
	instruction string
	// preserve are the parts of the spec the director asked to keep as they are (with the LOCKed ones).
	preserve []domain.SpecPart
	current  *domain.ShotSpec // the spec the Shot has, which the Candidates revise
	locks    domain.ParameterLocks
	total    int // most Takes this run makes (progress)
	// repaired is the retries the Repair history of the Shot counts (domain.RetriesDone) and earlier
	// is how many Takes Prompt Candidate Generation made for the Shot before this run: both count
	// against the Max retries per Shot together with the Takes of this run (retries).
	repaired, earlier int

	mu       sync.Mutex
	outcomes []judged
	// stopped says why the run made fewer Takes than it could (shown to the user).
	stopped string
	// prepared are Candidate specs already checked before the job was queued, in CandidateStrategies order.
	prepared []domain.ShotSpec
}

// generateCandidates runs Prompt Candidate Generation for a Shot (spec/spec.md §8). The mode follows
// from the Quality Policy (MAX QUALITY → Quality, BALANCED → Balanced, FAST → Economy):
//
//   - Quality Mode: the Prompt Planner writes a Candidate per shooting strategy, all of them are
//     generated at the same time and the Judge picks the Best Take.
//   - Balanced Mode: only the first Candidate (Stability First) is generated; while the Judge fails
//     the Take, the next Candidate is added, as far as the Retry Policy and the budget allow.
//   - Economy Mode: one Take; it is retaken only when the Judge finds a severe problem (a LOCK
//     violation or a high severity Issue), as far as the Retry Policy and the budget allow.
//
// Calls to the video models run in parallel; the Model Adapter limits how many are in flight per
// vendor according to its rate limit. The Best Take becomes the AI pick of the Shot and its spec
// the spec of the Shot.
// prepareCandidates plans the Prompt Candidate specs that are known before any Take is judged,
// and refuses a LOCK brand violation before the job is queued. Quality Mode plans every strategy
// it will shoot; other modes plan only the first, because the rest depend on the Judge.
// A nil slice means the job should plan them itself (the planner failed for another reason).
func (o *Orchestrator) prepareCandidates(ctx context.Context, ref repository.ShotRef, instruction string, preserve []domain.SpecPart, strategy Strategy) ([]domain.ShotSpec, error) {
	if err := refuseBusy(ref); err != nil {
		return nil, err // before the Prompt Planner is paid for
	}
	sc, err := o.loadShotContext(ctx, ref, instruction)
	if err != nil {
		return nil, err
	}
	locks, err := o.gens.ShotLocks(ctx, ref.Shot.ID)
	if err != nil {
		return nil, err
	}
	n := 1
	if mode := domain.ModeOf(sc.policy.Quality); mode == domain.ModeQuality {
		n = min(len(domain.CandidateStrategies()), plannedTakes(mode, sc.policy.Effective(), strategy))
	}
	var current *domain.ShotSpec
	if spec, ok := ref.Shot.CanonicalSpec(); ok {
		current = &spec
	}
	var prepared []domain.ShotSpec
	for _, s := range domain.CandidateStrategies()[:n] {
		in := sc.plannerInput(ref, instruction)
		in.Current, in.Strategy = current, s
		if current != nil {
			in.Fixed, in.Manual = candidateParts(locks, preserve, instruction)
		}
		spec, err := o.planner.Plan(ctx, in)
		if err != nil {
			if errors.Is(err, domain.ErrBrandViolation) {
				return nil, err
			}
			return nil, nil
		}
		prepared = append(prepared, spec)
	}
	return prepared, nil
}

func (o *Orchestrator) generateCandidates(ctx context.Context, ref repository.ShotRef, instruction string, preserve []domain.SpecPart, prepared []domain.ShotSpec) (Progress, error) {
	sc, err := o.loadShotContext(ctx, ref, instruction)
	if err != nil {
		return Progress{}, err
	}
	if err := sc.brand.CheckText(instruction); err != nil {
		return Progress{}, err
	}
	locks, err := o.gens.ShotLocks(ctx, ref.Shot.ID)
	if err != nil {
		return Progress{}, err
	}
	run := &candidateRun{o: o, ref: ref, sc: sc, instruction: instruction, preserve: preserve, locks: locks, prepared: prepared}
	if current, ok := ref.Shot.CanonicalSpec(); ok {
		run.current = &current
	}
	if run.repaired, run.earlier, err = run.priorTakes(ctx); err != nil {
		return Progress{}, err
	}
	first, err := run.strategy(ctx)
	if err != nil {
		return Progress{}, err
	}
	mode, eff := domain.ModeOf(sc.policy.Quality), sc.policy.Effective()
	run.total = plannedTakes(mode, eff, first)
	o.notify(Progress{ShotID: ref.Shot.ID, Action: ActionCandidates, Stage: StageGenerating, Message: mode.Label() + "で候補を生成しています", TakesTotal: run.total})

	var errs []error
	switch mode {
	case domain.ModeQuality:
		errs = run.quality(ctx, first)
	case domain.ModeBalanced:
		errs = run.balanced(ctx, first, eff)
	default:
		errs = run.economy(ctx, first, eff)
	}
	if len(run.outcomes) == 0 {
		return Progress{}, errors.Join(errs...)
	}
	if err := errors.Join(errs...); err != nil {
		o.logger.Warn("some candidates failed", "shot_id", ref.Shot.ID, "made", len(run.outcomes), "err", err)
	}
	pick := run.finish(ctx, mode)
	o.updateWorldState(ctx, ref, pick.spec, pick.takeID)
	return Progress{TakesDone: len(run.outcomes), TakesTotal: run.total}, nil
}

// priorTakes reads what the Shot has already spent on retries before this run: the retaking Repairs
// and the Takes of earlier Prompt Candidate Generation.
func (r *candidateRun) priorTakes(ctx context.Context) (repaired, earlier int, err error) {
	history, err := r.o.repairs.ShotActions(ctx, r.ref.Shot.ID)
	if err != nil {
		return 0, 0, err
	}
	earlier, err = r.o.repairs.CandidateTakeCount(ctx, r.ref.Shot.ID)
	if err != nil {
		return 0, 0, err
	}
	return domain.RetriesDone(history), earlier, nil
}

// retries is how many retries the Retry Policy counts for the Shot once this run has made `made`
// Takes: the same count the Repair loop uses (domain.RetriesDone + domain.CandidateRetries).
func (r *candidateRun) retries(made int) int {
	return r.repaired + domain.CandidateRetries(r.earlier+made)
}

// strategy decides the generation plan from the current budget.
func (r *candidateRun) strategy(ctx context.Context) (Strategy, error) {
	budget, err := r.o.gens.Budget(ctx, r.ref.ProjectID, r.ref.Shot.ID)
	if err != nil {
		return Strategy{}, err
	}
	return r.o.decide(ctx, r.ref, r.sc.policy, specOf(r.ref.Shot), budget)
}
