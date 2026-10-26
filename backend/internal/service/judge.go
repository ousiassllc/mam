package service

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// JudgeService runs the AI Video Judge on a Take (F-13) and returns its results.
type JudgeService struct {
	*core
	runner      *judge.Runner // nil when the Judge is not configured
	judges      *repository.JudgeRepository
	generations *GenerationService
}

// Judge queues a Judge run on the Take and returns its Shot in the JUDGING state (202). Every run
// adds a result; the earlier ones are kept. domain.CheckJudge refuses (409) a Take without a video,
// one that is being judged and a Shot that is being generated or repaired.
func (s *JudgeService) Judge(ctx context.Context, takeID string) (ShotFragment, error) {
	if s.runner == nil {
		return ShotFragment{}, ErrUnavailable
	}
	ref, err := s.judges.GetTake(ctx, takeID)
	if err != nil {
		return ShotFragment{}, notFound(err)
	}
	pending, err := s.judges.JudgePending(ctx, takeID)
	if err != nil {
		return ShotFragment{}, err
	}
	shot, err := s.generations.shots.GetShot(ctx, ref.Take.ShotID)
	if err != nil {
		return ShotFragment{}, notFound(err)
	}
	// The decision the Shot card draws its Judge button by.
	switch domain.CheckJudge(ref.Take, pending, shot.Shot.State) {
	case domain.JudgeNoVideo:
		return ShotFragment{}, ErrNoVideo
	case domain.JudgePending:
		return ShotFragment{}, ErrJudgeBusy
	case domain.JudgeBusy:
		return ShotFragment{}, ErrBusy
	}
	job, err := s.runner.Start(ctx, takeID)
	if err != nil {
		if errors.Is(err, judge.ErrBusy) {
			return ShotFragment{}, ErrJudgeBusy
		}
		return ShotFragment{}, notFound(err)
	}
	return s.generations.shotOfJob(ctx, ref.Take.ShotID, job)
}

// LatestResult returns the newest Judge result of the Take. ErrNotFound means the Take does not
// exist or has not been judged yet.
func (s *JudgeService) LatestResult(ctx context.Context, takeID string) (domain.JudgeResult, error) {
	res, err := s.judges.Latest(ctx, takeID)
	if err != nil {
		return domain.JudgeResult{}, notFound(err)
	}
	return res, nil
}
