package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// WorldService shows and edits the World State of a Project (F-09).
type WorldService struct {
	*core
	world *repository.WorldRepository
}

// WorldView is the world_state_page: the current World State with the fields the user has LOCKed.
type WorldView struct {
	Project domain.Project
	World   domain.WorldContext
	Locks   domain.LockSet
}

// View returns the current World State of the Project.
func (s *WorldService) View(ctx context.Context, projectID string) (WorldView, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return WorldView{}, err
	}
	world, err := s.world.World(ctx, projectID)
	if err != nil {
		return WorldView{}, notFound(err)
	}
	locks, err := s.world.Locks(ctx, projectID)
	if err != nil {
		return WorldView{}, err
	}
	return WorldView{Project: project, World: world, Locks: locks}, nil
}

// UpdateEntity sets the given fields (domain.EditableFields) of a Character, Product or Location
// and returns the new view. A LOCKed field is ErrLocked and nothing is written; a value that is
// too long, or an unknown field, is a ValidationError.
func (s *WorldService) UpdateEntity(ctx context.Context, projectID string, target domain.WorldTarget, id string, fields map[string]string) (WorldView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return WorldView{}, err
	}
	if len(fields) == 0 {
		return WorldView{}, invalid("", "更新する項目がありません")
	}
	clean := make(map[string]string, len(fields))
	for name, value := range fields {
		if !domain.IsEditable(target, name) {
			return WorldView{}, invalid(name, "この項目は編集できません")
		}
		value = strings.TrimSpace(value)
		raw, _ := json.Marshal(value)
		if err := domain.ValidateFieldValue(target, name, raw); err != nil {
			return WorldView{}, invalid(name, "%d 文字以内で入力してください", domain.MaxWorldValueRunes)
		}
		clean[name] = value
	}
	err := s.world.UpdateEntity(ctx, projectID, target, id, clean)
	switch {
	case errors.Is(err, repository.ErrLocked):
		return WorldView{}, ErrLocked
	case err != nil:
		return WorldView{}, notFound(err)
	}
	return s.View(ctx, projectID)
}
