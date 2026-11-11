package service

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/approvalack"
)

// ConfirmationSetting is the Confirmation Level with the CUSTOM CHECK selection.
type ConfirmationSetting struct {
	Level       domain.ConfirmationLevel
	Checkpoints []domain.Checkpoint
}

// defaultCustomCheckpoints is used when CUSTOM CHECK is chosen without a selection:
// the script and the final result.
var defaultCustomCheckpoints = []domain.Checkpoint{domain.CheckpointScript, domain.CheckpointFinal}

// SetConfirmationLevel changes when approval cards are shown (F-03) and records the
// change in the thread. For CUSTOM CHECK an empty selection keeps the previous one
// (or the default), so the setting never silently equals FULL AUTO.
func (s *ProjectService) SetConfirmationLevel(ctx context.Context, projectID string, level domain.ConfirmationLevel, checkpoints []domain.Checkpoint) (domain.Project, error) {
	if !level.Valid() {
		return domain.Project{}, invalid("level", "確認レベルが正しくありません")
	}
	for _, c := range checkpoints {
		if !c.Valid() {
			return domain.Project{}, invalid("checkpoints", "確認ポイントが正しくありません: %s", c)
		}
		if !c.Selectable() {
			return domain.Project{}, invalid("checkpoints", "この確認ポイントはまだ選べません: %s", c.Label())
		}
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	selected := checkpoints
	if level == domain.ConfirmCustomCheck && len(selected) == 0 {
		selected = project.CustomCheckpoints
		if len(selected) == 0 {
			selected = defaultCustomCheckpoints
		}
	}
	if level != domain.ConfirmCustomCheck {
		selected = project.CustomCheckpoints // remembered for when CUSTOM CHECK is chosen again
	}
	selected = sortedCheckpoints(selected)

	msgs := assignIDs([]domain.ChatMessage{assistantText(approvalack.ConfirmationNotice(level, selected))})
	err = s.projects.Apply(ctx, repository.Turn{
		ProjectID: projectID, Messages: msgs,
		Confirmation: &repository.Confirmation{Level: level, Checkpoints: selected},
	})
	if err != nil {
		return domain.Project{}, notFound(err)
	}
	s.publishCreated(projectID, msgs)
	s.kick(ctx, projectID) // a Brief waiting for confirmation does not wait at FULL AUTO (#1114)
	return s.getProject(ctx, projectID)
}

func sortedCheckpoints(in []domain.Checkpoint) []domain.Checkpoint {
	out := make([]domain.Checkpoint, 0, len(in))
	for _, c := range domain.SelectableCheckpoints() {
		if slices.Contains(in, c) {
			out = append(out, c)
		}
	}
	return out
}
