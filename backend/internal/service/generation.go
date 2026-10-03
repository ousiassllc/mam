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

// clipInstruction cuts a direction the user did not type into a form (the chat's, the AI's) to
// maxInstructionRunes, where a form field is refused with 422 instead.
func clipInstruction(s string) string {
	if utf8.RuneCountInString(s) <= maxInstructionRunes {
		return s
	}
	return string([]rune(s)[:maxInstructionRunes])
}

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
	if err := s.takeActions.mark(ctx, ref.ProjectID, []*ShotView{&view}); err != nil {
		return ShotFragment{}, err
	}
	return ShotFragment{ProjectID: ref.ProjectID, ShotView: view}, nil
}

// SubscribeShot streams the progress of the Shot (WebSocket /ws/shots/:id). The first message says
// what the Shot is doing now: a Shot being generated, judged or repaired gets its stage (a client that
// connects mid-work missed the earlier messages), any other Shot gets "idle" (a client that connects
// after the work ended must not wait for it).
//
// The subscription is made before the state is read, so a done / failed message published in between
// is received, not lost. A snapshot of a running Shot is queued only when no message has arrived
// since (that one is newer); "idle" goes behind them, as the last word on a Shot that is not running.
func (s *GenerationService) SubscribeShot(ctx context.Context, shotID string) (<-chan generation.Progress, func(), error) {
	return s.subscribeShot(ctx, shotID, s.shots.GetShot)
}

// subscribeShot is SubscribeShot reading the Shot through getShot (a seam for the test of the ordering).
func (s *GenerationService) subscribeShot(ctx context.Context, shotID string, getShot func(context.Context, string) (repository.ShotRef, error)) (<-chan generation.Progress, func(), error) {
	ch, cancel := s.progress.subscribe(shotID)
	ref, err := getShot(ctx, shotID)
	if err != nil {
		cancel()
		return nil, nil, notFound(err)
	}
	snapshot, running := progressSnapshot(ref.Shot)
	s.progress.sendTo(ch, snapshot, running)
	return ch, cancel, nil
}

// progressSnapshot is the progress message that matches the state of a Shot, and whether work is
// running on it. It uses the states the Shot card draws a Spinner for.
func progressSnapshot(shot domain.Shot) (generation.Progress, bool) {
	p := generation.Progress{ShotID: shot.ID, Stage: generation.StageIdle}
	switch shot.State {
	case domain.ShotGenerating:
		p.Stage = generation.StageGenerating
	case domain.ShotJudging:
		p.Action, p.Stage = generation.ActionJudge, generation.StageJudging
	case domain.ShotRepairing:
		p.Action, p.Stage = generation.ActionRepair, generation.StageRepairing
	default:
		return p, false
	}
	return p, true
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
