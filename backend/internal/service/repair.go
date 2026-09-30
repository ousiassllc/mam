package service

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// RepairService runs the Repair Agent on a Take (F-14) and reports the Jev shadow run.
type RepairService struct {
	*core
	agent       *repair.Agent // nil when generation, the Judge or the Reasoning LLM is not configured
	repairs     *repository.RepairRepository
	judges      *repository.JudgeRepository
	generations *GenerationService
}

// Repair queues a repair of the Take from its latest Judge result and returns its Shot in the
// REPAIRING state (202). The new Take is a new row; the repaired Take is not changed.
func (s *RepairService) Repair(ctx context.Context, takeID string) (ShotFragment, error) {
	if s.agent == nil {
		return ShotFragment{}, ErrUnavailable
	}
	ref, err := s.judges.GetTake(ctx, takeID)
	if err != nil {
		return ShotFragment{}, notFound(err)
	}
	if _, err := s.agent.Start(ctx, takeID, false); err != nil {
		switch {
		case errors.Is(err, repair.ErrNotJudged):
			return ShotFragment{}, ErrNotJudged
		case errors.Is(err, repair.ErrAccepted):
			return ShotFragment{}, ErrAccepted
		case errors.Is(err, repair.ErrBusy):
			return ShotFragment{}, ErrBusy
		}
		return ShotFragment{}, notFound(err)
	}
	s.recordIntervention(ctx, domain.Intervention{TakeID: takeID, Kind: domain.InterventionRepair})
	return s.generations.Shot(ctx, ref.Take.ShotID)
}

// ShadowReport returns the figures of the Jev shadow runs: agreement with the Repair Agent, the
// confidence distribution, latency and cost.
func (s *RepairService) ShadowReport(ctx context.Context) (domain.ShadowReport, error) {
	return s.repairs.ShadowReport(ctx)
}
