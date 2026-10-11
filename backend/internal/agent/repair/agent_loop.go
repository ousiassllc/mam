package repair

import (
	"context"
	"errors"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// step is one pass of the loop over a failed Take: what the Retry Policy and the strategy choice read.
type step struct {
	shot    repository.ShotRef
	auto    bool
	take    domain.Take
	res     domain.JudgeResult
	eff     domain.EffectivePolicy
	retries int
	history []domain.RepairRecord
	// regen is the regeneration the step would make: a full retake until a strategy is chosen.
	regen generation.Regeneration
}

// choice is the strategy chosen for a step, why, who decided, and what it was chosen from.
type choice struct {
	action     domain.RepairAction
	reason     string
	decider    domain.RepairDecider
	candidates []domain.RepairAction
	locks      domain.ParameterLocks
}

// run is one step of the loop: the Retry Policy, the strategy, the new Take. It reports whether a
// new Take was made.
func (a *Agent) run(ctx context.Context, shot repository.ShotRef, p payload) (bool, error) {
	if p.Partial != nil {
		return a.runPartial(ctx, shot, p)
	}
	st, err := a.newStep(ctx, shot, p)
	if err != nil {
		return false, err
	}
	// The Retry Policy (deterministic): Max retries per Shot, Max cost per Shot, the Project budget.
	if ok, err := a.withinPolicy(ctx, shot, st.eff, st.auto, st.retries, st.regen, st.res, st.take.ID); err != nil || !ok {
		return false, err
	}
	c, ok, err := a.chooseStrategy(ctx, st)
	if err != nil || !ok {
		return false, err
	}
	rec, err := a.Repairs.CreateAction(ctx, st.res.ID, c.action, domain.RepairInstructions{
		Instructions: st.regen.Instructions, Reason: c.reason, DecidedBy: c.decider, SourceTakeID: st.take.ID,
	})
	if err != nil {
		return false, err
	}

	var shadowDone sync.WaitGroup
	if c.decider == domain.DecidedByLLM && a.Shadow != nil {
		a.goShadow(context.WithoutCancel(ctx), &shadowDone, rec.ID, st.res, c.candidates, c.action)
	}
	defer shadowDone.Wait()

	if c.action == domain.RepairEditLevel {
		return false, a.editLevel(ctx, st, c.locks)
	}
	return a.regenerate(ctx, st, c.action, rec.ID)
}

// newStep reads what the Retry Policy and the strategy choice need: the failed Take, its Judge
// result, the effective policy and the retries the Shot has had.
func (a *Agent) newStep(ctx context.Context, shot repository.ShotRef, p payload) (*step, error) {
	take, err := a.Judges.GetTake(ctx, p.TakeID)
	if err != nil {
		return nil, err
	}
	res, err := a.Judges.Get(ctx, p.JudgeResultID)
	if err != nil {
		return nil, err
	}
	policy, err := a.Policies.Get(ctx, shot.ProjectID)
	if err != nil {
		return nil, err
	}
	history, err := a.Repairs.ShotActions(ctx, shot.Shot.ID)
	if err != nil {
		return nil, err
	}
	candidateTakes, err := a.Repairs.CandidateTakeCount(ctx, shot.Shot.ID)
	if err != nil {
		return nil, err
	}
	return &step{
		shot: shot, auto: p.Auto, take: take.Take, res: res, eff: policy.Effective(), history: history,
		// The Takes of Prompt Candidate Generation are retries too: the limit is over both flows.
		retries: domain.RetriesDone(history) + domain.CandidateRetries(candidateTakes),
		regen:   generation.Regeneration{Action: domain.RepairFullRetake, SourceModel: domain.ModelName(take.Take.ModelName)},
	}, nil
}

// chooseStrategy picks the repair strategy of the step and fills st.regen with it. It reports false
// (after the user was asked) when the Retry Policy does not allow the chosen strategy.
func (a *Agent) chooseStrategy(ctx context.Context, st *step) (choice, bool, error) {
	locks, err := a.Generations.ShotLocks(ctx, st.shot.Shot.ID)
	if err != nil {
		return choice{}, false, err
	}
	_, otherErr := a.Regenerator.RegenerationStrategy(ctx, st.shot, generation.Regeneration{Action: domain.RepairDifferentModel, SourceModel: st.regen.SourceModel})
	situation := Situation{
		Result: st.res, Tried: domain.TriedActions(st.history), HasReferences: len(st.shot.Shot.References()) > 0,
		PromptLocked: locks.Locked(domain.ParamPrompt), OtherModel: otherErr == nil,
	}
	candidates := Candidates(situation)
	action, reason, decider, extra := a.decide(ctx, situation, candidates)

	st.regen.Action = action
	st.regen.Issues, st.regen.Preserve = st.res.Issues, st.res.Preserve
	st.regen.Instructions = mergeInstructions(st.res, extra)
	if action.Regenerates() && action != domain.RepairFullRetake {
		// The cost of the chosen strategy can differ (another model): apply the Retry Policy to it.
		if ok, err := a.withinPolicy(ctx, st.shot, st.eff, st.auto, st.retries, st.regen, st.res, st.take.ID); err != nil || !ok {
			return choice{}, false, err
		}
	}
	return choice{action: action, reason: reason, decider: decider, candidates: candidates, locks: locks}, true, nil
}

// regenerate makes the new Take of the chosen strategy, links it to the repair action, splices a
// Partial Regeneration into the failed Take, and tells the user. It reports whether a Take was made.
func (a *Agent) regenerate(ctx context.Context, st *step, action domain.RepairAction, actionID string) (bool, error) {
	a.Notify(generation.Progress{ShotID: st.shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageRepairing, Message: action.Label() + " で新しい Take を作成しています"})
	// A Partial Regeneration whose Issues all name a time range is spliced into the failed Take: only
	// those ranges of the new Take are used.
	var ranges []domain.TimeRange
	if action == domain.RepairPartialRegeneration && a.Splicer != nil {
		if r, ok := domain.IssueRanges(st.res.Issues); ok {
			ranges = r
		}
	}
	st.regen.Ranges = ranges
	newTake, err := a.Regenerator.Regenerate(ctx, st.shot, st.regen)
	if err != nil {
		return false, err
	}
	if err := a.Repairs.SetResultingTake(context.WithoutCancel(ctx), actionID, newTake); err != nil {
		return false, err
	}
	if len(ranges) > 0 {
		a.splice(ctx, st.shot, st.take.ID, newTake, ranges)
	}
	a.announceRegenerated(ctx, st, action)
	return true, nil
}

// editLevel is the Edit-Level Repair: no new Take, the user is told which ranges to fix in the edit.
func (a *Agent) editLevel(ctx context.Context, st *step, locks domain.ParameterLocks) error {
	adopted, err := a.adoptForEdit(ctx, st.shot, st.take.ID, locks)
	if err != nil {
		return err
	}
	return a.announceEditLevel(ctx, st.shot, st.take.ID, st.res, adopted)
}

// adoptForEdit makes the Take the Shot's Selected Take when an Edit-Level Repair keeps it and the AI
// owns the Shot's Take (AUTO mode, Take Selection AUTO, no Take selected yet). Edit-Level Repair makes
// no new Take, so without this the Shot would stay without a Selected Take and the AUTO editing chain
// (Rough Cut once every Shot has one) would wait for ever. It reports whether the Take was selected.
func (a *Agent) adoptForEdit(ctx context.Context, shot repository.ShotRef, takeID string, locks domain.ParameterLocks) (bool, error) {
	if a.Takes == nil || shot.Shot.SelectedTakeID != "" || locks.Mode(domain.ParamTakeSelection) != domain.LockAuto {
		return false, nil
	}
	project, err := a.Projects.Get(ctx, shot.ProjectID)
	if err != nil {
		return false, err
	}
	if project.Mode != domain.ModeAuto {
		return false, nil
	}
	if _, err := a.Takes.Select(context.WithoutCancel(ctx), takeID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return false, nil // its file was archived: the user has to look at the Shot
		}
		return false, err
	}
	return true, nil
}

// decide chooses the strategy: the deterministic rules first, then the Repair Agent (LLM) among the
// candidates, then the fallback when it cannot answer. It returns the strategy, why, who decided
// and the extra instructions of the Repair Agent.
func (a *Agent) decide(ctx context.Context, s Situation, candidates []domain.RepairAction) (domain.RepairAction, string, domain.RepairDecider, []string) {
	if action, reason, ok := Rule(s, candidates); ok {
		return action, reason, domain.DecidedByRule, nil
	}
	if a.LLM != nil {
		c, err := choose(ctx, a.LLM, s, candidates)
		if err == nil {
			return c.Action, c.Reason, domain.DecidedByLLM, c.Instructions
		}
		if ctx.Err() == nil {
			a.Logger.Warn("repair agent could not choose; using the fallback", "err", err)
		}
	}
	action, reason := Fallback(s, candidates)
	return action, reason, domain.DecidedByRule, nil
}

// mergeInstructions joins the Judge's instructions with the Repair Agent's, without repeats.
func mergeInstructions(res domain.JudgeResult, extra []string) []string {
	var list []string
	if rs := res.RepairStrategy; rs != nil {
		list = append(list, rs.Instructions...)
	}
	return identifiers(append(list, extra...))
}
