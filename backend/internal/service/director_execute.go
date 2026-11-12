package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/approvalack"
)

// execute carries out the operations of a Command: an operation that asks for confirmation (set_budget) becomes a confirmation card and is carried out by the
// answer (director_ops.go). The rest: settings first (so that "カメラは固定して、表情だけ
// 変えて" locks before it regenerates), then regenerations, then the mode switch. strict makes a
// regeneration that would change a LOCKed parameter fail with ErrLocked before anything is done;
// otherwise it is left out and reported.
func (s *DirectorService) execute(ctx context.Context, project domain.Project, ref *repository.ShotRef, cmd commandinterpreter.Command, message string, strict bool) (outcome, error) {
	var out outcome
	if cmd.Question != "" { // the model asks; what the message plainly asks for (WithDetected) is still carried out
		out.lines = []string{cmd.Question}
		if len(cmd.Operations) == 0 {
			return out, nil
		}
	}
	// Without a Shot (nothing says which one is meant) the operations on a Shot wait for the answer to askWhichShot; the others of the same Command (a budget card, a setting,
	// a note) are carried out, not dropped with them.
	askShot := false
	if ref == nil {
		before := len(cmd.Operations)
		cmd.Operations = slices.DeleteFunc(slices.Clone(cmd.Operations), needsShot)
		askShot = len(cmd.Operations) < before
	}

	var locks domain.ParameterLocks
	if ref != nil {
		var err error
		if locks, err = s.locks.ShotLocks(ctx, ref.Shot.ID); err != nil {
			return out, err
		}
		if strict {
			if err := lockedRegeneration(locks, cmd.Operations); err != nil {
				return out, err
			}
		}
	}

	for _, op := range cmd.Operations {
		if op.Type != commandinterpreter.OpSetLock {
			continue
		}
		line, err := s.setLock(ctx, project.ID, ref, op)
		if err != nil {
			return out, err
		}
		out.lines = append(out.lines, line)
	}
	if ref != nil {
		var err error
		if locks, err = s.locks.ShotLocks(ctx, ref.Shot.ID); err != nil {
			return out, err
		}
	}
	for _, op := range cmd.Operations {
		switch op.Type {
		case commandinterpreter.OpRegenerate, commandinterpreter.OpPartialRegenerate:
			line, started, err := s.regenerate(ctx, ref, locks, op)
			if err != nil {
				return out, err
			}
			out.lines = append(out.lines, line)
			out.started = out.started || started
		case commandinterpreter.OpCreateBranch:
			lines, err := s.branch.Fork(ctx, *ref, op.PointType, op.Count, op.Directions)
			out.lines = append(out.lines, lines...)
			if err != nil {
				return out, err
			}
		case commandinterpreter.OpUnsupported:
			out.lines = append(out.lines, unsupportedReply(message, op.Reason))
		}
	}
	if err := s.confirmAndRemember(ctx, project, cmd, &out); err != nil {
		return out, err
	}
	err := s.switchMode(ctx, project, cmd, &out)
	if askShot && err == nil {
		out.lines = append(out.lines, askWhichShot)
	}
	return out, err
}

// needsShot reports whether the operation is carried out on a Shot.
func needsShot(op commandinterpreter.Operation) bool {
	return op.Regenerates() || op.Type == commandinterpreter.OpCreateBranch || (op.Type == commandinterpreter.OpSetLock && op.Parameter.ValidForShot())
}

// confirmAndRemember posts the confirmation cards of the operations that ask for confirmation (director_ops.go), then keeps the notes the director asked for.
func (s *DirectorService) confirmAndRemember(ctx context.Context, project domain.Project, cmd commandinterpreter.Command, out *outcome) error {
	for _, op := range cmd.Operations {
		if op.Immediate() { // nothing changes: what it found (a download link) is told at once
			res, err := s.applyOp(ctx, project, op)
			if err != nil {
				return err
			}
			out.lines = append(out.lines, res.text)
			out.cards = append(out.cards, res.messages...)
			continue
		}
		if !op.Confirms() {
			continue
		}
		card, line, err := s.confirmCard(ctx, project, op)
		if err != nil {
			return err
		}
		if line != "" {
			out.lines = append(out.lines, line)
		} else {
			out.cards = append(out.cards, card)
		}
	}
	for _, op := range cmd.Operations {
		if op.Type != commandinterpreter.OpCreateNote {
			continue
		}
		line, err := s.remember(ctx, project.ID, op)
		if err != nil {
			return err
		}
		out.lines = append(out.lines, line)
	}
	return nil
}

// switchMode carries out the mode switches of the Command.
func (s *DirectorService) switchMode(ctx context.Context, project domain.Project, cmd commandinterpreter.Command, out *outcome) error {
	mode := project.Mode
	for _, op := range cmd.Operations {
		if op.Type == commandinterpreter.OpSetMode {
			if op.ProjectMode == mode {
				// nothing is switched: no switch_to_director is recorded, no pipeline is kicked
				out.lines = append(out.lines, "すでに "+strings.ToUpper(string(mode))+" MODE です。")
				continue
			}
			mode = op.ProjectMode
			if err := s.applyMode(ctx, project.ID, op.ProjectMode, false); err != nil {
				return err
			}
			out.lines = append(out.lines, approvalack.ModeNotice(op.ProjectMode))
			if op.ProjectMode == domain.ModeAuto {
				s.kick(ctx, project.ID)
			}
		}
	}
	return nil
}

// lockedRegeneration returns ErrLocked when a regeneration of ops changes a parameter that is
// LOCKed once the ops' own settings are applied.
func lockedRegeneration(locks domain.ParameterLocks, ops []commandinterpreter.Operation) error {
	effective := domain.ParameterLocks{}
	for p, v := range locks {
		effective[p] = v
	}
	for _, op := range ops {
		if op.Type == commandinterpreter.OpSetLock && op.Parameter.ValidForShot() {
			effective[op.Parameter] = domain.LockSetting{Mode: op.Mode}
		}
	}
	var hit []domain.LockParameter
	for _, op := range ops {
		if op.Regenerates() {
			hit = append(hit, lockedParameters(effective, op)...)
		}
	}
	if len(hit) > 0 {
		return &LockedError{Parameters: hit}
	}
	return nil
}

// lockedParameters lists the LOCKed parameters that the regeneration changes: the parameter of
// each changed part, and Prompt, which covers the whole spec.
func lockedParameters(locks domain.ParameterLocks, op commandinterpreter.Operation) []domain.LockParameter {
	var hit []domain.LockParameter
	for _, part := range op.Changes {
		if p, ok := part.Parameter(); ok && locks.Locked(p) {
			hit = append(hit, p)
		}
	}
	if locks.Locked(domain.ParamPrompt) && !slices.Contains(hit, domain.ParamPrompt) {
		hit = append(hit, domain.ParamPrompt)
	}
	return hit
}

func (s *DirectorService) setLock(ctx context.Context, projectID string, ref *repository.ShotRef, op commandinterpreter.Operation) (string, error) {
	if op.Parameter.ValidForProject() {
		if _, err := s.SetProjectLock(ctx, projectID, op.Parameter, op.Mode); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s を %s にしました。", op.Parameter.Label(), op.Mode.Label()), nil
	}
	if op.Parameter == domain.ParamVideoModel && op.Mode == domain.LockManual && op.Value == "" {
		return "Video Model を MANUAL にするにはモデル名（" + domain.JoinModels(domain.VideoModels(), " / ") + "）を指定してください。", nil
	}
	if _, err := s.SetShotLock(ctx, ref.Shot.ID, op.Parameter, op.Mode, op.Value); err != nil {
		if errors.Is(err, ErrLocked) {
			return fmt.Sprintf("Shot %d の %s は LOCK 中の値を別の値に変えられません。先に AUTO か MANUAL に切り替えてください。", ref.Shot.OrderIndex+1, op.Parameter.Label()), nil
		}
		return "", err
	}
	return fmt.Sprintf("Scene %d / Shot %d の %s を %s にしました。", ref.Scene.OrderIndex+1, ref.Shot.OrderIndex+1, op.Parameter.Label(), op.Mode.Label()), nil
}
