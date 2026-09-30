package generation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// penalty is the weight of the Issues of a verdict: the sum of their severity ranks, a LOCK
// violation counting as much as a high severity Issue plus one.
func penalty(r domain.JudgeResult) int {
	n := 0
	for _, is := range r.Issues {
		n += is.Severity.Rank()
		if is.LockViolation {
			n += domain.SeverityHigh.Rank() + 1
		}
	}
	return n
}

// better reports whether a is the better Take than b: a Take that passed beats one that failed, then
// the one with the lighter Issues wins, then the one the Judge is more confident about, then the
// earlier one. A Take that could not be judged is worse than any judged one.
func better(a, b judged) bool {
	switch {
	case a.result == nil || b.result == nil:
		return a.result != nil && b.result == nil
	case (a.result.Result == domain.JudgePass) != (b.result.Result == domain.JudgePass):
		return a.result.Result == domain.JudgePass
	case penalty(*a.result) != penalty(*b.result):
		return penalty(*a.result) < penalty(*b.result)
	case a.result.OverallConfidence != b.result.OverallConfidence:
		return a.result.OverallConfidence > b.result.OverallConfidence
	}
	return a.number < b.number
}

// bestTake returns the Best Take among the judged ones. ok is false when none could be judged.
func bestTake(all []judged) (best judged, ok bool) {
	for _, j := range all {
		if j.result == nil {
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
	current     *domain.ShotSpec // the spec the Shot has, which the Candidates revise
	locks       domain.ParameterLocks
	total       int // most Takes this run makes (progress)

	mu       sync.Mutex
	outcomes []judged
	// stopped says why the run made fewer Takes than it could (shown to the user).
	stopped string
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
func (o *Orchestrator) generateCandidates(ctx context.Context, ref repository.ShotRef, instruction string) (Progress, error) {
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
	run := &candidateRun{o: o, ref: ref, sc: sc, instruction: instruction, locks: locks}
	if current, ok := ref.Shot.CanonicalSpec(); ok {
		run.current = &current
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

// strategy decides the generation plan from the current budget.
func (r *candidateRun) strategy(ctx context.Context) (Strategy, error) {
	budget, err := r.o.gens.Budget(ctx, r.ref.ProjectID, r.ref.Shot.ID)
	if err != nil {
		return Strategy{}, err
	}
	return r.o.decide(ctx, r.ref, r.sc.policy, specOf(r.ref.Shot), budget)
}

// plan has the Prompt Planner write the Candidate of the strategy.
func (r *candidateRun) plan(ctx context.Context, strategy domain.CandidateStrategy) (domain.ShotSpec, error) {
	in := r.sc.plannerInput(r.ref, r.instruction)
	in.Current, in.Strategy = r.current, strategy
	if r.current != nil {
		in.Fixed, in.Manual = r.locks.LockedParts(), r.locks.PartsWith(domain.LockManual)
	}
	return r.o.planner.Plan(ctx, in)
}

// take generates a Take for the spec, has the Judge evaluate it and records the outcome. A Take
// the Judge could not evaluate is kept, without a verdict.
func (r *candidateRun) take(ctx context.Context, s Strategy, strategy domain.CandidateStrategy, spec domain.ShotSpec, number int) (judged, error) {
	o := r.o
	job, err := o.videoJob(ctx, r.ref.ProjectID, spec, r.sc.brief, s.DurationSeconds, r.sc.references)
	if err != nil {
		return judged{}, err
	}
	record, err := recordOf(s, job, strategy)
	if err != nil {
		return judged{}, err
	}
	pv, err := o.gens.SaveCandidateSpec(ctx, r.ref.Shot.ID, spec, record, domain.NoteRefs(r.sc.notes))
	if err != nil {
		return judged{}, err
	}
	takeID, err := o.makeTake(ctx, r.ref, pv, s, job, domain.TakeLineage{WorldStateVersion: r.sc.world.Version, References: r.sc.references}, fmt.Sprintf("Shot %d · Take %d", r.ref.Shot.OrderIndex+1, number), r.sc.policy.Effective().MaxGenerationTime, strategy)
	if err != nil {
		return judged{}, err
	}
	out := judged{takeID: takeID, number: number, strategy: strategy, spec: spec}
	r.mu.Lock()
	r.outcomes = append(r.outcomes, out)
	made := len(r.outcomes)
	r.mu.Unlock()
	o.notify(Progress{ShotID: r.ref.Shot.ID, Action: ActionCandidates, Stage: StageGenerating, Message: "動画を生成しました。Judge が評価しています", TakesDone: made, TakesTotal: r.total})

	res, err := o.judge.Evaluate(ctx, takeID)
	if err != nil {
		o.logger.Warn("judge candidate failed", "take_id", takeID, "err", err)
		return out, nil
	}
	out.result = &res
	r.mu.Lock()
	for i := range r.outcomes {
		if r.outcomes[i].takeID == takeID {
			r.outcomes[i] = out
		}
	}
	r.mu.Unlock()
	return out, nil
}

// quality plans every Candidate and generates them at the same time.
func (r *candidateRun) quality(ctx context.Context, s Strategy) []error {
	strategies := domain.CandidateStrategies()[:r.total]
	return parallel(r.o.logger, len(strategies), func(i int) error {
		spec, err := r.plan(ctx, strategies[i])
		if err != nil {
			return err
		}
		_, err = r.take(ctx, s, strategies[i], spec, i+1)
		return err
	})
}

// balanced generates the first Candidate and adds the next while the Judge fails the last Take.
func (r *candidateRun) balanced(ctx context.Context, s Strategy, eff domain.EffectivePolicy) []error {
	strategies := domain.CandidateStrategies()
	for i := range r.total {
		if i > 0 {
			var ok bool
			if s, ok = r.mayRetry(ctx, eff, i-1); !ok {
				return nil
			}
		}
		spec, err := r.plan(ctx, strategies[i])
		if err != nil {
			return []error{err}
		}
		out, err := r.take(ctx, s, strategies[i], spec, i+1)
		if err != nil {
			return []error{err}
		}
		if out.result == nil || out.result.Result == domain.JudgePass {
			return nil // it is good, or it cannot be judged and nothing more is spent on it
		}
	}
	return nil
}

// economy generates one Take and retakes it after a severe problem only.
func (r *candidateRun) economy(ctx context.Context, s Strategy, eff domain.EffectivePolicy) []error {
	strategy := domain.CandidateStabilityFirst
	spec, err := r.plan(ctx, strategy)
	if err != nil {
		return []error{err}
	}
	for retakes := 0; ; retakes++ {
		out, err := r.take(ctx, s, strategy, spec, retakes+1)
		if err != nil {
			return []error{err}
		}
		if out.result == nil || !severe(*out.result) {
			return nil
		}
		var ok bool
		if s, ok = r.mayRetry(ctx, eff, retakes); !ok {
			return nil
		}
	}
}

// mayRetry applies the Retry Policy and the budget before one more Take. When it says no, the reason
// is kept for the summary. retriesDone is the number of extra Takes made so far.
func (r *candidateRun) mayRetry(ctx context.Context, eff domain.EffectivePolicy, retriesDone int) (Strategy, bool) {
	budget, err := r.o.gens.Budget(ctx, r.ref.ProjectID, r.ref.Shot.ID)
	if err != nil {
		r.o.logger.Warn("read budget failed", "shot_id", r.ref.Shot.ID, "err", err)
		return Strategy{}, false
	}
	s, err := r.o.decide(ctx, r.ref, r.sc.policy, specOf(r.ref.Shot), budget)
	if err != nil {
		r.stop("予算上限に達したため、追加の生成は行いませんでした")
		return Strategy{}, false
	}
	switch eff.CanRetry(retriesDone, budget.ShotSpent, budget.ProjectSpent, s.CostPerTake) {
	case domain.RetryAllowed:
		return s, true
	case domain.RetryMaxRetries:
		r.stop("リトライ上限（Max retries）に達したため、追加の生成は行いませんでした")
	default:
		r.stop("予算上限に達したため、追加の生成は行いませんでした")
	}
	return Strategy{}, false
}

func (r *candidateRun) stop(reason string) {
	r.mu.Lock()
	r.stopped = reason
	r.mu.Unlock()
}

// finish picks the Best Take, makes it the AI pick and its spec the spec of the Shot, and tells the
// user. The Takes exist and cost money, so a failure to store the pick is logged and never fails
// the job. It returns the Take the run continues from (the Best Take, or the first one when none
// could be judged).
func (r *candidateRun) finish(ctx context.Context, mode domain.GenerationMode) judged {
	o, shot := r.o, r.ref.Shot
	bg := context.WithoutCancel(ctx)
	// Concurrent Takes finish in any order: list them in Take order.
	all := append([]judged(nil), r.outcomes...)
	slices.SortFunc(all, func(a, b judged) int { return a.number - b.number })

	pick, judgedAny := bestTake(all)
	if !judgedAny {
		pick = all[0]
	} else if err := o.gens.MarkAIPick(bg, shot.ID, pick.takeID); err != nil {
		o.logger.Warn("mark ai pick failed", "take_id", pick.takeID, "err", err)
	}
	if err := o.gens.AdoptSpec(bg, shot.ID, pick.spec); err != nil {
		o.logger.Warn("adopt candidate spec failed", "shot_id", shot.ID, "err", err)
	}
	notice := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: r.summary(mode, all, pick, judgedAny)}
	if err := o.chat.Announce(bg, r.ref.ProjectID, notice); err != nil {
		o.logger.Error("post candidate summary failed", "shot_id", shot.ID, "err", err)
	}
	return pick
}

// summary is the chat notice that closes a run: every Take with its verdict and the Best Take.
func (r *candidateRun) summary(mode domain.GenerationMode, all []judged, pick judged, judgedAny bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Shot %d: %s で %d 件の Take を生成しました。", r.ref.Shot.OrderIndex+1, mode.Label(), len(all))
	for _, j := range all {
		verdict := "未評価"
		if j.result != nil {
			verdict = "FAIL"
			if j.result.Result == domain.JudgePass {
				verdict = "PASS"
			}
			verdict += fmt.Sprintf("（信頼度 %.2f）", j.result.OverallConfidence)
		}
		fmt.Fprintf(&b, "\n・Take %d %s: %s", j.number, j.strategy.Label(), verdict)
	}
	switch {
	case !judgedAny:
		b.WriteString("\nJudge で評価できた Take がないため、最良の Take は選べませんでした。Judge を実行して選んでください。")
	case pick.result.Result == domain.JudgePass:
		fmt.Fprintf(&b, "\nJudge の結果、Take %d（%s）を最良の Take として推奨します。", pick.number, pick.strategy.Label())
	default:
		fmt.Fprintf(&b, "\n基準を満たす Take はありませんでした。最も問題の少ない Take %d（%s）を推奨します。", pick.number, pick.strategy.Label())
	}
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if stopped != "" {
		b.WriteString("\n" + stopped + "。")
	}
	return b.String()
}
