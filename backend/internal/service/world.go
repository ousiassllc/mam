package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
	// ProjectLocks is the AUTO / MANUAL / LOCK setting of Story and Music.
	ProjectLocks domain.ParameterLocks
	// Library is the Reference Library of the Project: the images a reference slot can hold.
	Library []domain.ReferenceItem
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
	projectLocks, err := s.director.locks.ProjectLocks(ctx, projectID)
	if err != nil {
		return WorldView{}, err
	}
	library, err := s.references.List(ctx, projectID, repository.ReferenceFilter{})
	if err != nil {
		return WorldView{}, err
	}
	return WorldView{Project: project, World: world, Locks: locks, ProjectLocks: projectLocks, Library: library}, nil
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

// SetFieldLock switches one attribute of the World State between AUTO and LOCK (F-09, spec/spec.md
// §9.3) and returns the new view. id is the Character, Product or Location; Camera and Story have
// none. An unknown target, attribute or mode is a ValidationError; a LOCKed attribute is not
// updated by the State Extractor and cannot be edited.
func (s *WorldService) SetFieldLock(ctx context.Context, projectID string, target domain.WorldTarget, id, field string, mode domain.LockMode) (WorldView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return WorldView{}, err
	}
	if !slices.Contains(domain.WorldTargets(), target) {
		return WorldView{}, invalid("target_type", "対象の種類が正しくありません")
	}
	if !domain.IsLockable(target, field) {
		return WorldView{}, invalid("parameter", "この項目は LOCK できません")
	}
	if mode != domain.LockAuto && mode != domain.LockLocked {
		return WorldView{}, invalid("mode", "AUTO か LOCK を指定してください")
	}
	if err := s.world.SetFieldLock(ctx, projectID, target, id, field, mode); err != nil {
		return WorldView{}, notFound(err)
	}
	return s.View(ctx, projectID)
}

// AssignReference puts the Reference Library image on a reference slot of a Character, Product or
// Location (referenceID "" empties the slot) and returns the new view (spec/spec.md §10.1). A slot
// the target has none of, or an image of the wrong kind, is a ValidationError; a slot whose LOCK is
// set is ErrLocked.
func (s *WorldService) AssignReference(ctx context.Context, projectID string, target domain.WorldTarget, id, slotName, referenceID string) (WorldView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return WorldView{}, err
	}
	slot, ok := domain.FindReferenceSlot(target, slotName)
	if !ok {
		return WorldView{}, invalid("slot", "この対象には指定の参照枠がありません")
	}
	assetID := ""
	if referenceID = strings.TrimSpace(referenceID); referenceID != "" {
		item, err := s.references.Get(ctx, projectID, referenceID)
		if err != nil {
			return WorldView{}, notFound(err)
		}
		if item.Kind != slot.Kind {
			return WorldView{}, invalid("reference_id", "%s の参照画像を選んでください", slot.Kind.Label())
		}
		assetID = item.AssetID
	}
	err := s.world.AssignReference(ctx, projectID, slot, id, assetID)
	switch {
	case errors.Is(err, repository.ErrLocked):
		return WorldView{}, ErrLocked
	case err != nil:
		return WorldView{}, notFound(err)
	}
	return s.View(ctx, projectID)
}
