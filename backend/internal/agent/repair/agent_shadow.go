package repair

import (
	"context"
	"errors"
	"runtime/debug"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// goShadow runs runShadow in the background, counted in done. The measurement is optional, so a
// panic in it is logged with its stack and stops there: it must not take the server down.
func (a *Agent) goShadow(ctx context.Context, done *sync.WaitGroup, actionID string, res domain.JudgeResult, candidates []domain.RepairAction, decided domain.RepairAction) {
	done.Add(1)
	go func() {
		defer done.Done()
		defer func() {
			if r := recover(); r != nil {
				a.Logger.Error("repair shadow panicked", "action_id", actionID, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		a.runShadow(ctx, actionID, res, candidates, decided)
	}()
}

// runShadow asks Jev the question the Repair Agent just answered and records the outcome. Nothing
// it does or fails to do reaches the repair. Only structured Issue fields are sent.
func (a *Agent) runShadow(ctx context.Context, actionID string, res domain.JudgeResult, candidates []domain.RepairAction, decided domain.RepairAction) {
	out, err := a.Shadow.Choose(ctx, jev.Input{Issues: res.Issues, Candidates: candidates})
	if errors.Is(err, jev.ErrNoAPIKey) {
		return // Jev is not set up: nothing to measure
	}
	rec := domain.RepairShadow{RepairActionID: actionID, Model: a.Shadow.Model(), DecidedAction: decided}
	if err != nil {
		rec.Error = err.Error()
	} else {
		rec.Model, rec.JevChoice, rec.Probabilities = firstNonEmpty(out.Model, rec.Model), string(out.Action), out.Probabilities
		rec.Confidence = &out.Confidence
		rec.Agreed = out.Action == decided
		rec.LatencyMillis, rec.InputTokens, rec.OutputTokens, rec.CostUSD = out.Latency.Milliseconds(), out.InputTokens, out.OutputTokens, out.CostUSD
	}
	if rec.Error != "" {
		a.Logger.Warn("jev shadow run failed", "err", rec.Error)
	}
	if err := a.Repairs.SaveShadow(ctx, rec); err != nil {
		a.Logger.Error("save jev shadow run failed", "err", err)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
