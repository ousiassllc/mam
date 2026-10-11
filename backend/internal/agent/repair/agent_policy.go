package repair

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// withinPolicy applies the Retry Policy to the next regeneration of regen.Action. It returns false
// (after asking the user, unless the automatic loop already did) when the policy stops the loop.
func (a *Agent) withinPolicy(ctx context.Context, shot repository.ShotRef, eff domain.EffectivePolicy, auto bool, retries int, regen generation.Regeneration, res domain.JudgeResult, takeID string) (bool, error) {
	verdict, err := a.verdict(ctx, shot, eff, retries, regen)
	if err != nil {
		return false, err
	}
	if verdict == domain.RetryAllowed {
		a.escalated.Forget(shot.Shot.ID) // the user raised the limit or the budget: nothing waits for an answer
		return true, nil
	}
	if a.escalated.Ask(shot.Shot.ID, res.ID) && auto {
		return false, nil // the user was already asked about this result; a manual Repair always asks again
	}
	if err := a.escalate(ctx, shot, takeID, res, verdict); err != nil {
		a.escalated.Forget(shot.Shot.ID) // the question never reached the user: the next automatic run asks again
		return false, err
	}
	return false, nil
}

// verdict is the Retry Policy's answer for regen. A retry that no budget covers is a budget verdict.
func (a *Agent) verdict(ctx context.Context, shot repository.ShotRef, eff domain.EffectivePolicy, retries int, regen generation.Regeneration) (domain.RetryVerdict, error) {
	if retries >= eff.MaxRetries {
		return domain.RetryMaxRetries, nil // decided before any cost is estimated
	}
	strategy, err := a.Regenerator.RegenerationStrategy(ctx, shot, regen)
	exceeded := errors.Is(err, generation.ErrBudgetExceeded)
	if err != nil && !exceeded {
		return "", err
	}
	budget, err := a.Generations.Budget(ctx, shot.ProjectID, shot.Shot.ID)
	if err != nil {
		return "", err
	}
	if exceeded {
		return budgetVerdict(eff, budget), nil
	}
	return eff.CanRetry(retries, budget.ShotSpent, budget.ProjectSpent, strategy.CostPerTake), nil
}

// budgetVerdict names the ceiling that ends the loop when not even one Take fits the budget: the one
// with the least money left (the Shot's on a tie), so the Escalation does not blame the Shot's
// ceiling when the Project's budget is what ran out.
func budgetVerdict(eff domain.EffectivePolicy, b repository.Budget) domain.RetryVerdict {
	switch {
	case eff.ShotBudgetCeiling == nil:
		return domain.RetryProjectBudget
	case eff.ProjectBudgetCeiling == nil:
		return domain.RetryShotBudget
	case *eff.ProjectBudgetCeiling-b.ProjectSpent < *eff.ShotBudgetCeiling-b.ShotSpent:
		return domain.RetryProjectBudget
	}
	return domain.RetryShotBudget
}
