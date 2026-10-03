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

// takeActions answers "may this Take be repaired / judged now" for the pages that draw a Repair or a
// Judge button. The decisions are domain.CheckRepair and domain.CheckJudge, the ones the server refuses
// by (POST /takes/:id/repair and /judge are 409 otherwise), so that a button is drawn exactly when the
// server accepts the request.
type takeActions struct {
	judges   *repository.JudgeRepository
	policies *repository.PolicyRepository
	// judgeEnabled is whether the Judge is configured; without it POST /takes/:id/judge is always 503.
	judgeEnabled bool
}

// mark fills Repairable and Judgeable of the views of one Project. The Retry Policy is read once, and
// only when a Shot has Takes. Judgeable holds the Takes that were never judged (a failed run stores no
// result) and may be judged now: a judged Take is evaluated, so offering the run again would only
// crowd the row.
func (r takeActions) mark(ctx context.Context, projectID string, views []*ShotView) error {
	var policy *domain.ProjectPolicy
	for _, v := range views {
		v.Repairable, v.Judgeable = nil, nil
		for _, t := range v.Takes {
			if policy == nil {
				p, err := r.policies.Get(ctx, projectID)
				if err != nil {
					return err
				}
				policy = &p
			}
			var latest *domain.JudgeResult
			res, err := r.judges.Latest(ctx, t.ID)
			switch {
			case err == nil:
				latest = &res
			case !errors.Is(err, repository.ErrNotFound):
				return err
			}
			if domain.CheckRepair(latest, policy.Effective(), policy.Template, v.State) == domain.RepairAllowed {
				if v.Repairable == nil {
					v.Repairable = map[string]bool{}
				}
				v.Repairable[t.ID] = true
			}
			if latest != nil || !r.judgeEnabled || t.Status == domain.TakeRejected { // an excluded Take is not offered a run
				continue
			}
			pending, err := r.judges.JudgePending(ctx, t.ID)
			if err != nil {
				return err
			}
			if domain.CheckJudge(t, pending, v.State) == domain.JudgeAllowed {
				if v.Judgeable == nil {
					v.Judgeable = map[string]bool{}
				}
				v.Judgeable[t.ID] = true
			}
		}
	}
	return nil
}
