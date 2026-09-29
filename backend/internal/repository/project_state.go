package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrInvalidTransition is returned when a Core State Machine move is not allowed (spec/spec.md §22).
var ErrInvalidTransition = errors.New("repository: invalid state transition")

// Transition moves the Project to another Core State Machine state and persists it.
// Moving to the current state is a no-op. Any other move MUST be allowed by
// domain.CanTransition, otherwise ErrInvalidTransition is returned and nothing changes.
// It returns ErrNotFound when the Project does not exist.
func (r *ProjectRepository) Transition(ctx context.Context, projectID string, to domain.ProjectState) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var from domain.ProjectState
		err := tx.QueryRowContext(ctx, `SELECT state FROM project WHERE id = ?`, projectID).Scan(&from)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if from == to {
			return nil
		}
		if !domain.CanTransition(from, to) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
		}
		_, err = tx.ExecContext(ctx, `UPDATE project SET state = ? WHERE id = ?`, to, projectID)
		return err
	})
	if err != nil {
		return fmt.Errorf("transition project: %w", err)
	}
	return nil
}
