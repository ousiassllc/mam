package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// refuseBrand returns ErrBrandViolation when a regeneration's instruction (the user's words, or the
// English direction the interpreter produced) or the direction of a Branch to create contains a
// forbidden expression of a LOCK constraint.
// Advisory constraints are not an error. The Shot Spec is checked again inside the generation that
// would be queued, still before the job exists.
func (s *DirectorService) refuseBrand(ctx context.Context, projectID, message string, cmd commandinterpreter.Command) error {
	generates := func(op commandinterpreter.Operation) bool {
		return op.Regenerates() || op.Type == commandinterpreter.OpCreateBranch
	}
	if s.brands == nil || !slices.ContainsFunc(cmd.Operations, generates) {
		return nil
	}
	list, err := s.brands.List(ctx, projectID)
	if err != nil {
		return err
	}
	if err := list.CheckText(message); err != nil {
		return err
	}
	for _, op := range cmd.Operations {
		if !generates(op) {
			continue
		}
		for _, text := range append([]string{op.Instruction}, op.Directions...) {
			if err := list.CheckText(text); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *DirectorService) regenerate(ctx context.Context, ref *repository.ShotRef, locks domain.ParameterLocks, op commandinterpreter.Operation) (string, bool, error) {
	label := fmt.Sprintf("Scene %d / Shot %d", ref.Scene.OrderIndex+1, ref.Shot.OrderIndex+1)
	if hit := lockedParameters(locks, op); len(hit) > 0 {
		names := make([]string, len(hit))
		for i, p := range hit {
			names[i] = p.Label()
		}
		return fmt.Sprintf("%s の %s は LOCK されているため変更しませんでした。変えたい場合は LOCK を解除するよう指示してください。", label, strings.Join(names, " / ")), false, nil
	}
	// The interpreter does not know the Shot's length: a range that starts after the Shot ends is
	// refused here, before a whole new Take is paid for.
	if op.Type == commandinterpreter.OpPartialRegenerate && ref.Shot.Duration > 0 {
		if i := slices.IndexFunc(op.Ranges, func(r domain.TimeRange) bool { return r.From >= ref.Shot.Duration }); i >= 0 {
			return fmt.Sprintf("%s は %.1f 秒の Shot で、%s は Shot の長さを超えているため再生成しませんでした。Shot の長さ以内の時間範囲で指示してください。",
				label, ref.Shot.Duration, domain.FormatTimeRanges(op.Ranges[i:i+1])), false, nil
		}
	}
	instruction := clipInstruction(op.Instruction)
	partial := op.Type == commandinterpreter.OpPartialRegenerate
	var err error
	switch {
	case partial:
		err = s.startPartial(ctx, ref, op, instruction)
		if err == nil {
			s.recordIntervention(ctx, domain.Intervention{ShotID: ref.Shot.ID, Kind: domain.InterventionRepair})
		}
	case s.orch == nil:
		return "", false, ErrUnavailable
	default:
		record := s.generationRecorder(ctx, ref.Shot.ID)
		if _, err = s.orch.StartByPolicy(ctx, ref.Shot.ID, instruction, op.Preserve); err == nil {
			record()
		}
	}
	if err != nil {
		if errors.Is(err, errNoTakeToFix) {
			return label + " には再生成の元にできる Take がまだありません。先に Shot を生成してください。", false, nil
		}
		if errors.Is(err, repair.ErrSpliceUnavailable) {
			return label + " の時間範囲だけの再生成には ffmpeg が必要ですが、見つかりませんでした。", false, nil
		}
		err = mapGenerationError(err)
		switch {
		case errors.Is(err, ErrBusy):
			return label + " は生成中のため、終わってからもう一度指示してください。", false, nil
		case errors.Is(err, ErrBudgetExceeded):
			return label + " は予算上限を超えるため再生成できません。", false, nil
		case errors.Is(err, ErrBrandViolation):
			return "", false, err
		}
		return "", false, err
	}
	text := label
	if partial {
		text += " の " + domain.FormatTimeRanges(op.Ranges) + " だけを再生成して、元の Take につなぎ込みます"
	} else {
		text += " だけを再生成します"
	}
	if len(op.Changes) > 0 {
		text += "（変更: " + partLabels(op.Changes)
		if len(op.Preserve) > 0 {
			text += " / 保持: " + partLabels(op.Preserve)
		}
		text += "）"
	}
	return text + "。", true, nil
}

// errNoTakeToFix means the Shot has no Take a Partial Regeneration could fix.
var errNoTakeToFix = errors.New("service: the shot has no take to regenerate from")

// startPartial queues the Partial Regeneration of op for the Take the Shot uses: the one the user
// selected, else the AI Pick, else the newest one that is not rejected.
func (s *DirectorService) startPartial(ctx context.Context, ref *repository.ShotRef, op commandinterpreter.Operation, instruction string) error {
	if s.repair == nil {
		return ErrUnavailable
	}
	takes, err := s.takes.ShotTakes(ctx, ref.Shot.ID)
	if err != nil {
		return err
	}
	base, ok := domain.TakeToRepair(takes)
	if !ok {
		return errNoTakeToFix
	}
	_, err = s.repair.StartPartial(ctx, base, repair.PartialRequest{Ranges: op.Ranges, Instruction: instruction, Preserve: op.Preserve})
	return err
}

func partLabels(parts []domain.SpecPart) string {
	labels := make([]string, len(parts))
	for i, p := range parts {
		labels[i] = p.Label()
	}
	return strings.Join(labels, ", ")
}
