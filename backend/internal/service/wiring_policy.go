package service

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// policyService is the service of the budget and the Quality Policy: the settings screen, MCP and the chat's budget operations share this one.
func (w *wiring) policyService() *PolicyService {
	if w.policy == nil {
		w.policy = &PolicyService{
			core: w.c, freeBytes: w.deps.FreeBytes, policies: w.r.policies, costs: w.r.costs, storage: repository.NewStorageRepository(w.deps.DB),
		}
	}
	return w.policy
}

// wireDirectorPolicy gives the Director the service that sets the budget and the Quality Policy (the one the settings screen uses), and the gate's reading of the
// models whose price is not known (#1139).
func (w *wiring) wireDirectorPolicy() {
	w.director.policy = w.policyService()
	w.director.unpriced = func(ctx context.Context, projectID string) ([]string, error) {
		if !w.gate.On() {
			return nil, nil
		}
		launches, err := w.c.genApprovals.Launches(ctx, projectID)
		if err != nil {
			return nil, err
		}
		return w.gate.PlanBuilder().UnpricedModels(ctx, projectID, launches)
	}
}
