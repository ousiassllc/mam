package service

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// maxInstructionRunes bounds the additional instruction of a generation.
const maxInstructionRunes = 1000

// GenerationService starts the generation of a Shot (Prompt Autopilot → Model Adapter → Takes, F-06,
// F-07, F-12) and the building of its Reference Pack (F-11), and streams their progress.
type GenerationService struct {
	*core
	gen      *generation.Orchestrator // nil when generation is not configured
	shots    *repository.GenerationRepository
	progress *broker[generation.Progress]
}

// ShotFragment is a Shot with its Project, for the fragment an action answers with.
type ShotFragment struct {
	ProjectID string
	ShotView
}

// GenerateShot queues the generation of the Shot and returns it in the GENERATING state (202).
// instruction is an optional additional direction ("もう少しカメラを寄せて").
func (s *GenerationService) GenerateShot(ctx context.Context, shotID, instruction string) (ShotFragment, error) {
	if s.gen == nil {
		return ShotFragment{}, ErrUnavailable
	}
	if utf8.RuneCountInString(instruction) > maxInstructionRunes {
		return ShotFragment{}, invalid("additional_instruction", "%d 文字以内で入力してください", maxInstructionRunes)
	}
	record := s.generationRecorder(ctx, shotID)
	if _, err := s.gen.Start(ctx, shotID, instruction); err != nil {
		return ShotFragment{}, mapGenerationError(err)
	}
	record()
	return s.Shot(ctx, shotID)
}

// GenerateCandidates queues Prompt Candidate Generation for the Shot (F-08) and returns it in the
// GENERATING state (202). The Generation Mode follows from the Quality Policy of the Project.
// instruction is an optional additional direction. ErrUnavailable means generation or the AI Video
// Judge (which picks the Best Take) is not configured.
func (s *GenerationService) GenerateCandidates(ctx context.Context, shotID, instruction string) (ShotFragment, error) {
	if s.gen == nil {
		return ShotFragment{}, ErrUnavailable
	}
	if utf8.RuneCountInString(instruction) > maxInstructionRunes {
		return ShotFragment{}, invalid("additional_instruction", "%d 文字以内で入力してください", maxInstructionRunes)
	}
	record := s.generationRecorder(ctx, shotID)
	if _, err := s.gen.StartCandidates(ctx, shotID, instruction); err != nil {
		return ShotFragment{}, mapGenerationError(err)
	}
	record()
	return s.Shot(ctx, shotID)
}

// BuildReferencePack queues the (re)generation of the Shot's Reference Pack and returns the Shot.
func (s *GenerationService) BuildReferencePack(ctx context.Context, shotID string) (ShotFragment, error) {
	if s.gen == nil {
		return ShotFragment{}, ErrUnavailable
	}
	if _, err := s.gen.StartReferencePack(ctx, shotID); err != nil {
		return ShotFragment{}, mapGenerationError(err)
	}
	return s.Shot(ctx, shotID)
}

// Shot returns the Shot with its Takes.
func (s *GenerationService) Shot(ctx context.Context, shotID string) (ShotFragment, error) {
	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return ShotFragment{}, notFound(err)
	}
	takes, err := s.plans.ListTakes(ctx, ref.Scene.ID)
	if err != nil {
		return ShotFragment{}, err
	}
	view := ShotView{Shot: ref.Shot, Takes: takes[ref.Shot.ID], Selected: true}
	if frame, ok := ref.Shot.Storyboard(); ok {
		view.Storyboard = &frame
	}
	return ShotFragment{ProjectID: ref.ProjectID, ShotView: view}, nil
}

// SubscribeShot streams the progress of the Shot (WebSocket /ws/shots/:id). The first message says
// whether something is running now (a client that connects after the work ended must not wait for it).
func (s *GenerationService) SubscribeShot(ctx context.Context, shotID string) (<-chan generation.Progress, func(), error) {
	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return nil, nil, notFound(err)
	}
	ch, cancel := s.progress.Subscribe(shotID)
	switch ref.Shot.State {
	case domain.ShotGenerating, domain.ShotJudging, domain.ShotRepairing:
		// Work is in progress: the card keeps listening for its messages.
	default:
		s.progress.Publish(shotID, generation.Progress{ShotID: shotID, Stage: generation.StageIdle})
	}
	return ch, cancel, nil
}

func mapGenerationError(err error) error {
	switch {
	case errors.Is(err, generation.ErrShotBusy):
		return ErrBusy
	case errors.Is(err, generation.ErrBudgetExceeded):
		return ErrBudgetExceeded
	case errors.Is(err, generation.ErrJudgeUnavailable):
		return ErrUnavailable
	case errors.Is(err, repository.ErrLocked):
		return lockedError(err)
	}
	return notFound(err)
}
