package repair

import (
	"context"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Choose exposes choose to the tests.
var Choose = choose

// GoShadow exposes goShadow to the tests.
func (a *Agent) GoShadow(ctx context.Context, done *sync.WaitGroup, actionID string, res domain.JudgeResult, candidates []domain.RepairAction, decided domain.RepairAction) {
	a.goShadow(ctx, done, actionID, res, candidates, decided)
}

// WithinPolicy exposes withinPolicy to the tests.
func (a *Agent) WithinPolicy(ctx context.Context, shot repository.ShotRef, eff domain.EffectivePolicy, auto bool, retries int, res domain.JudgeResult, takeID string) (bool, error) {
	return a.withinPolicy(ctx, shot, eff, auto, retries, generation.Regeneration{}, res, takeID)
}

// Decide exposes decide to the tests.
func (a *Agent) Decide(ctx context.Context, s Situation, candidates []domain.RepairAction) (domain.RepairAction, string, domain.RepairDecider, []string) {
	return a.decide(ctx, s, candidates)
}
