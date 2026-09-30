package repair

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrSpliceUnavailable means a Partial Regeneration by time range cannot be made: joining the new
// Take into the old one needs ffmpeg, which is not installed (503).
var ErrSpliceUnavailable = errors.New("repair: splicing takes needs ffmpeg")

// PartialRequest is a Partial Regeneration the director asks for (spec/spec.md §13): fix the time
// ranges of a Take, keep everything else.
type PartialRequest struct {
	// Ranges are the stretches of the Take to fix (seconds). At least one.
	Ranges []domain.TimeRange `json:"ranges"`
	// Instruction is the director's direction for the Prompt Planner (English).
	Instruction string `json:"instruction,omitempty"`
	// Preserve are the parts of the Shot Spec that must stay as they are.
	Preserve []domain.SpecPart `json:"preserve,omitempty"`
	// Spec is the revised Canonical Shot Spec, already checked against LOCK brand constraints.
	// NoteRefs are the production notes that spec was planned from.
	Spec     *domain.ShotSpec `json:"spec,omitempty"`
	NoteRefs []domain.NoteRef `json:"note_refs,omitempty"`
}

// partialPreparer checks a Partial Regeneration against LOCK brand constraints before it is queued.
type partialPreparer interface {
	PreparePartial(context.Context, repository.ShotRef, generation.Regeneration) (domain.ShotSpec, []domain.NoteRef, bool, error)
}

// StartPartial queues a Partial Regeneration of the Take: a new Take of the whole Shot is made under
// the instruction, keeping the Preserve parts and the LOCKed ones, and its Ranges are spliced into
// the Take. Both are added as new Takes; the Take itself is not changed. The Retry Policy does not
// apply (the director asked), but the budget does. It returns ErrSpliceUnavailable without ffmpeg,
// ErrBusy when the Shot is being generated or repaired, generation.ErrBudgetExceeded when not even
// one Take fits the budget, a *repository.LockedParametersError when the Prompt is LOCKed, and
// repository.ErrNotFound for an unknown Take.
func (a *Agent) StartPartial(ctx context.Context, takeID string, req PartialRequest) (domain.Job, error) {
	if len(req.Ranges) == 0 {
		return domain.Job{}, errors.New("repair: partial regeneration needs a time range")
	}
	ref, err := a.Judges.GetTake(ctx, takeID)
	if err != nil {
		return domain.Job{}, err
	}
	if ref.Take.FilePath == "" {
		return domain.Job{}, errors.New("repair: the take has no file")
	}
	shot, err := a.Generations.GetShot(ctx, ref.Take.ShotID)
	if err != nil {
		return domain.Job{}, err
	}
	if prep, ok := a.Regenerator.(partialPreparer); ok {
		preserve := make([]string, len(req.Preserve))
		for i, part := range req.Preserve {
			preserve[i] = string(part)
		}
		spec, notes, checked, err := prep.PreparePartial(ctx, shot, generation.Regeneration{
			Action: domain.RepairPartialRegeneration, Direction: req.Instruction,
			Ranges: req.Ranges, Preserve: preserve,
		})
		if err != nil {
			return domain.Job{}, err
		}
		if checked {
			req.Spec, req.NoteRefs = &spec, notes
		}
	}
	if a.Splicer == nil {
		return domain.Job{}, ErrSpliceUnavailable
	}
	locks, err := a.Generations.ShotLocks(ctx, shot.Shot.ID)
	if err != nil {
		return domain.Job{}, err
	}
	if locks.Locked(domain.ParamPrompt) {
		return domain.Job{}, &repository.LockedParametersError{Parameters: []domain.LockParameter{domain.ParamPrompt}}
	}
	if _, err := a.Regenerator.RegenerationStrategy(ctx, shot, generation.Regeneration{Action: domain.RepairPartialRegeneration}); err != nil {
		return domain.Job{}, err
	}
	claimed, err := a.Generations.StartRepair(ctx, shot.Shot.ID)
	if err != nil {
		return domain.Job{}, err
	}
	if !claimed {
		return domain.Job{}, ErrBusy
	}
	job, err := a.Jobs.Enqueue(ctx, domain.JobRepair, payload{TakeID: takeID, ShotID: shot.Shot.ID, Partial: &req})
	if err != nil {
		a.release(ctx, shot.Shot.ID)
		return domain.Job{}, fmt.Errorf("enqueue partial regeneration job: %w", err)
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindProgress, RefID: job.ID,
		Content: fmt.Sprintf("Shot %d の %s だけを再生成しています", shot.Shot.OrderIndex+1, domain.FormatTimeRanges(req.Ranges)),
	}
	if err := a.Chat.Announce(ctx, shot.ProjectID, card); err != nil {
		a.Logger.Error("post partial regeneration progress card failed", "shot_id", shot.Shot.ID, "job_id", job.ID, "err", err)
	}
	a.Notify(generation.Progress{ShotID: shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageQueued})
	return job, nil
}

// runPartial makes the Partial Regeneration of a director's request (see StartPartial).
func (a *Agent) runPartial(ctx context.Context, shot repository.ShotRef, p payload) (bool, error) {
	take, err := a.Judges.GetTake(ctx, p.TakeID)
	if err != nil {
		return false, err
	}
	req := p.Partial
	preserve := make([]string, len(req.Preserve))
	for i, part := range req.Preserve {
		preserve[i] = string(part)
	}
	a.Notify(generation.Progress{ShotID: shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageRepairing, Message: domain.FormatTimeRanges(req.Ranges) + " を再生成して新しい Take を作成しています"})
	newTake, err := a.Regenerator.Regenerate(ctx, shot, generation.Regeneration{
		Action: domain.RepairPartialRegeneration, Preserve: preserve, SourceModel: domain.ModelName(take.Take.ModelName),
		Ranges: req.Ranges, Direction: req.Instruction, Prepared: req.Spec, NoteRefs: req.NoteRefs,
	})
	if err != nil {
		return false, err
	}
	a.splice(ctx, shot, p.TakeID, newTake, req.Ranges)
	return true, nil
}

// splice adds the Take that shows baseTakeID outside the ranges and the regenerated Take inside
// them, and has it judged. The regenerated Take stays a Take of its own, so a splice that fails or is
// not worth making does not fail the repair: the user is told and has the regenerated Take.
func (a *Agent) splice(ctx context.Context, shot repository.ShotRef, baseTakeID, regeneratedTakeID string, ranges []domain.TimeRange) {
	ctx = context.WithoutCancel(ctx)
	no := shot.Shot.OrderIndex + 1
	splicedID, err := a.Splicer.SpliceRegenerated(ctx, shot.Shot.ID, baseTakeID, regeneratedTakeID, ranges)
	var text string
	switch {
	case err != nil:
		a.Logger.Error("splice partial regeneration failed", "shot_id", shot.Shot.ID, "take_id", regeneratedTakeID, "err", err)
		text = fmt.Sprintf("Shot %d の %s をつなぎ込めませんでした（%s）。再生成した Take は Shot 全体の Take として残っています。", no, domain.FormatTimeRanges(ranges), err)
	case splicedID == "":
		text = fmt.Sprintf("Shot %d は %s が Shot のほとんどを占めるため、つなぎ込まず再生成した Take をそのまま残しました。", no, domain.FormatTimeRanges(ranges))
	default:
		text = fmt.Sprintf("Shot %d の %s だけを再生成した Take を、元の Take につなぎ込みました。つなぎ込んだ Take を Judge します（元の Take と再生成した Take はそのまま残っています）。", no, domain.FormatTimeRanges(ranges))
		if a.Judge != nil {
			if _, err := a.Judge.Start(ctx, splicedID); err != nil {
				a.Logger.Error("start judge of spliced take failed", "take_id", splicedID, "err", err)
			}
		}
	}
	if err := a.Chat.Announce(ctx, shot.ProjectID, domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: text}); err != nil {
		a.Logger.Error("post splice notice failed", "shot_id", shot.Shot.ID, "err", err)
	}
}
