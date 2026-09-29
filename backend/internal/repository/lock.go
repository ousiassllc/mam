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
	rows, err := q.QueryContext(ctx, `SELECT parameter_name, mode, COALESCE(value, 'null') FROM lock WHERE `+where, args...) //nolint:gosec // where is a constant of this file; only the arguments are data
	if err != nil {
		return nil, fmt.Errorf("list parameter locks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	locks := domain.ParameterLocks{}
	for rows.Next() {
		var (
			name, mode, value string
		)
		if err := rows.Scan(&name, &mode, &value); err != nil {
			return nil, err
		}
		locks[domain.LockParameter(name)] = domain.LockSetting{Mode: domain.LockMode(mode), Value: json.RawMessage(value)}
	}
	return locks, rows.Err()
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
