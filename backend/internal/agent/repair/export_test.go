package repair

import (
	"context"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Choose exposes choose to the tests.
var Choose = choose

// GoShadow exposes goShadow to the tests.
func (a *Agent) GoShadow(ctx context.Context, done *sync.WaitGroup, actionID string, res domain.JudgeResult, candidates []domain.RepairAction, decided domain.RepairAction) {
	a.goShadow(ctx, done, actionID, res, candidates, decided)
}
