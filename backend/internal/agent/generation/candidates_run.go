package generation

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/agegate"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// candidateParts are the parts of the Shot's spec a Prompt Candidate must keep (fixed) and the parts
// the user specified that it should keep (manual) when the Prompt Planner writes it. The strategy of a
// Candidate rewrites the spec, so the parts the user set himself (MANUAL) are carried like the single
// generation carries them: with no instruction the spec is not rewritten there, so they stay exactly
// as they are, and they are fixed here too; an instruction may change them, so they are only a hint.
func candidateParts(locks domain.ParameterLocks, preserve []domain.SpecPart, instruction string) (fixed, manual []domain.SpecPart) {
	fixed, manual = slices.Concat(locks.LockedParts(), preserve), locks.PartsWith(domain.LockManual)
	if strings.TrimSpace(instruction) == "" {
		return slices.Concat(fixed, manual), nil
	}
	return fixed, manual
}

// plan has the Prompt Planner write the Candidate of the strategy.
func (r *candidateRun) plan(ctx context.Context, strategy domain.CandidateStrategy) (domain.ShotSpec, error) {
	for i, s := range domain.CandidateStrategies() {
		if s == strategy && i < len(r.prepared) {
			return r.prepared[i], nil
		}
	}
	in := r.sc.plannerInput(r.ref, r.instruction)
	in.Current, in.Strategy = r.current, strategy
	if r.current != nil {
		in.Fixed, in.Manual = candidateParts(r.locks, r.preserve, r.instruction)
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
			if s, ok = r.mayRetry(ctx, eff, r.retries(i)); !ok {
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
		if s, ok = r.mayRetry(ctx, eff, r.retries(retakes+1)); !ok {
			return nil
		}
	}
}

// mayRetry applies the Retry Policy and the budget before one more Take. When it says no, the reason
// is kept for the summary. retriesDone is the retries the Shot has used so far (retries).
func (r *candidateRun) mayRetry(ctx context.Context, eff domain.EffectivePolicy, retriesDone int) (Strategy, bool) {
	budget, err := r.o.gens.Budget(ctx, r.ref.ProjectID, r.ref.Shot.ID)
	if err != nil {
		r.o.logger.Warn("read budget failed", "shot_id", r.ref.Shot.ID, "err", err)
		return Strategy{}, false
	}
	s, err := r.o.decide(ctx, r.ref, r.sc.policy, specOf(r.ref.Shot), budget)
	if err != nil {
		r.stop(cmp.Or(agegate.Reason(err), "予算上限に達したため、追加の生成は行いませんでした")) // the gate closed meanwhile, else the budget
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

	eff, template := r.sc.policy.Effective(), r.sc.policy.Template
	pick, judgedAny := bestTake(all, func(res domain.JudgeResult) bool { return res.Accepted(eff, template) })
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
