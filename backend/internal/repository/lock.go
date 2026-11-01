package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// LockedParametersError is ErrLocked with the LOCKed parameters the change touched.
type LockedParametersError struct{ Parameters []domain.LockParameter }

func (e *LockedParametersError) Error() string {
	return fmt.Sprintf("%v: %v", ErrLocked, e.Parameters)
}

// Is makes errors.Is(err, ErrLocked) hold.
func (e *LockedParametersError) Is(target error) bool { return target == ErrLocked }

// LockRepository persists the AUTO / MANUAL / LOCK setting of the parameters of Shots and Projects
// (spec/spec.md §6). Every setting is a row of the lock table, the one place LOCK is judged
// (spec/architecture/er.md §2.2). All writes go through DB.Write.
type LockRepository struct{ db *DB }

// NewLockRepository returns a LockRepository backed by db.
func NewLockRepository(db *DB) *LockRepository { return &LockRepository{db: db} }

// ShotLocks returns the setting of the parameters of the Shot.
func (r *LockRepository) ShotLocks(ctx context.Context, shotID string) (domain.ParameterLocks, error) {
	return readParameterLocks(ctx, r.db.Reader(), `target_type = ? AND target_id = ?`, domain.LockTargetShot, shotID)
}

// ProjectLocks returns the setting of the Project's own parameters (Story and Music).
func (r *LockRepository) ProjectLocks(ctx context.Context, projectID string) (domain.ParameterLocks, error) {
	return readParameterLocks(ctx, r.db.Reader(), `project_id = ? AND target_type IN ('story', 'music')`, projectID)
}

// readParameterLocks reads the rows matching where; the parameter of a row is its parameter_name.
func readParameterLocks(ctx context.Context, q queryer, where string, args ...any) (domain.ParameterLocks, error) {
	type paramLock struct {
		name    domain.LockParameter
		setting domain.LockSetting
	}
	rows, err := queryAll(ctx, q, func(row rowScanner) (paramLock, error) {
		var name, mode, value string
		if err := row.Scan(&name, &mode, &value); err != nil {
			return paramLock{}, err
		}
		return paramLock{domain.LockParameter(name), domain.LockSetting{Mode: domain.LockMode(mode), Value: json.RawMessage(value)}}, nil
	}, `SELECT parameter_name, mode, COALESCE(value, 'null') FROM lock WHERE `+where, args...) //nolint:gosec // where is a constant of this file; only the arguments are data
	if err != nil {
		return nil, fmt.Errorf("list parameter locks: %w", err)
	}
	locks := make(domain.ParameterLocks, len(rows))
	for _, l := range rows {
		locks[l.name] = l.setting
	}
	return locks, nil
}

// SetShotLock sets the mode of a parameter of the Shot (an unknown Shot is ErrNotFound). value is
// the JSON value kept with it (the model name of Video Model). LOCKing Video Model without a value
// freezes the model of the Shot's latest Take, when it has one.
func (r *LockRepository) SetShotLock(ctx context.Context, shotID string, p domain.LockParameter, mode domain.LockMode, value json.RawMessage) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var projectID string
		err := tx.QueryRowContext(ctx, `SELECT s.project_id FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE sh.id = ?`, shotID).Scan(&projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if mode == domain.LockLocked && len(value) > 0 {
			// A LOCKed value cannot be replaced by another one while it stays LOCKed.
			var oldMode, oldValue string
			err := tx.QueryRowContext(ctx,
				`SELECT mode, COALESCE(value, 'null') FROM lock WHERE project_id = ? AND target_type = ? AND target_id = ? AND parameter_name = ?`,
				projectID, domain.LockTargetShot, shotID, string(p)).Scan(&oldMode, &oldValue)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if oldMode == string(domain.LockLocked) && oldValue != "null" && !domain.SameValue(json.RawMessage(oldValue), value) {
				return &LockedParametersError{Parameters: []domain.LockParameter{p}}
			}
		}
		if p == domain.ParamVideoModel && mode == domain.LockLocked && len(value) == 0 {
			var model string
			err := tx.QueryRowContext(ctx,
				`SELECT t.model_name FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
				 WHERE pv.shot_id = ? ORDER BY t.created_at DESC, t.id DESC LIMIT 1`, shotID).Scan(&model)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if model != "" {
				value, _ = json.Marshal(model)
			}
		}
		return upsertLock(ctx, tx, projectID, domain.LockTargetShot, shotID, string(p), mode, value)
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLocked) {
		return err
	}
	if err != nil {
		return fmt.Errorf("set shot lock: %w", err)
	}
	return nil
}

// SetProjectLock sets the mode of Story or Music (an unknown Project is ErrNotFound).
func (r *LockRepository) SetProjectLock(ctx context.Context, projectID string, p domain.LockParameter, mode domain.LockMode) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		return upsertLock(ctx, tx, projectID, string(p), projectID, string(p), mode, nil)
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("set project lock: %w", err)
	}
	return nil
}

func upsertLock(ctx context.Context, tx *sql.Tx, projectID, targetType, targetID, parameter string, mode domain.LockMode, value json.RawMessage) error {
	var stored any
	if len(value) > 0 {
		stored = string(value)
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode, value) VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (project_id, target_type, target_id, parameter_name) DO UPDATE SET mode = excluded.mode, value = excluded.value`,
		domain.NewID(), projectID, targetType, targetID, parameter, mode, stored)
	return err
}

// Locks returns the fields of the Project that are LOCKed: the ones the user cannot edit either.
func (r *WorldRepository) Locks(ctx context.Context, projectID string) (domain.LockSet, error) {
	return readLocks(ctx, r.db.Reader(), projectID)
}

// AgentLocks returns the fields the State Extractor must not change: the LOCKed ones and the MANUAL ones (spec/spec.md §6).
func (r *WorldRepository) AgentLocks(ctx context.Context, projectID string) (domain.LockSet, error) {
	return readLockRows(ctx, r.db.Reader(), projectID, domain.LockMode.KeepsTheAgentsOut)
}

// readLocks reads the lock rows in mode 'lock' (the singleton Camera and Story regardless of target_id).
func readLocks(ctx context.Context, q queryer, projectID string) (domain.LockSet, error) {
	return readLockRows(ctx, q, projectID, func(m domain.LockMode) bool { return m == domain.LockLocked })
}

// readLockRows reads the lock rows whose mode holds.
func readLockRows(ctx context.Context, q queryer, projectID string, holds func(domain.LockMode) bool) (domain.LockSet, error) {
	type lockRow struct {
		key  domain.LockKey
		mode domain.LockMode
	}
	rows, err := queryAll(ctx, q, func(row rowScanner) (lockRow, error) {
		var l lockRow
		return l, row.Scan(&l.key.Target, &l.key.ID, &l.key.Field, &l.mode)
	}, `SELECT target_type, target_id, parameter_name, mode FROM lock WHERE project_id = ? AND mode <> 'auto'`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list locks: %w", err)
	}
	locks := domain.LockSet{}
	for _, l := range rows {
		if !holds(l.mode) {
			continue
		}
		k := l.key
		if k.Target.IsSingleton() {
			k.ID = ""
		}
		locks[k] = struct{}{}
	}
	return locks, nil
}

// SetFieldLock sets an attribute of the World State to AUTO or LOCK (a lock row of the target; the singleton Camera and
// Story use the Project's ID as target_id). The attribute is not checked here (see domain.IsLockable). An unknown Project,
// or an entity that is not in the trunk (or the Branch when branchID is set), is ErrNotFound.
func (r *WorldRepository) SetFieldLock(ctx context.Context, projectID, branchID string, target domain.WorldTarget, id, field string, mode domain.LockMode) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		targetID := projectID
		if !target.IsSingleton() {
			if err := entityExists(ctx, tx, projectID, branchID, target, id); err != nil {
				return err
			}
			targetID = id
		} else if err := branchExists(ctx, tx, projectID, branchID); err != nil {
			return err
		}
		return upsertLock(ctx, tx, projectID, string(target), targetID, field, mode, nil)
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("set %s lock: %w", target, err)
	}
	return nil
}

func entityExists(ctx context.Context, q queryer, projectID, branchID string, target domain.WorldTarget, id string) error {
	table, ok := worldTables[target]
	if !ok {
		return fmt.Errorf("%s is not an entity target", target)
	}
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ? AND project_id = ? AND branch_id IS ?`, id, projectID, nullIfEmpty(branchID)).Scan(&one) //nolint:gosec // table is from worldTables
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// AssignReference sets the image of a reference slot of a Character, Product or Location (the asset of a Reference
// Library entry; "" clears the slot) and appends the resulting World State as a new version of the trunk (or the Branch).
// The entity is ErrNotFound when it is not in that trunk or Branch; the write is ErrLocked when slot.Lock is LOCKed.
func (r *WorldRepository) AssignReference(ctx context.Context, projectID, branchID string, slot domain.ReferenceSlot, id, assetID string) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := entityExists(ctx, tx, projectID, branchID, slot.Target, id); err != nil {
			return err
		}
		locks, err := readLocks(ctx, tx, projectID)
		if err != nil {
			return err
		}
		if locks.Locked(slot.Target, id, slot.Lock) {
			return ErrLocked
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+worldTables[slot.Target]+` SET `+slot.Column+` = ? WHERE id = ?`, nullIfEmpty(assetID), id); err != nil { //nolint:gosec // table and column are constants of domain.ReferenceSlots; only the values are data
			return err
		}
		w, err := readWorld(ctx, tx, projectID, branchID)
		if err != nil {
			return err
		}
		_, _, err = r.appendVersion(ctx, tx, projectID, branchID, w)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLocked) {
		return err
	}
	if err != nil {
		return fmt.Errorf("assign %s reference: %w", slot.Target, err)
	}
	return nil
}
