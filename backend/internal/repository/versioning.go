package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// VersionRepository lists and restores the versions of Reversible Decisions (F-28, spec/spec.md
// §29.4): the prompt versions of a Shot, the World State versions of the trunk or of a Branch, and
// the versions of a Branch itself. Versions are append-only, so a restore never deletes anything:
// it copies the chosen version into a new one at the end of the series and makes that current.
// All writes go through DB.Write.
type VersionRepository struct {
	db  *DB
	now func() time.Time
}

// NewVersionRepository returns a VersionRepository backed by db.
func NewVersionRepository(db *DB) *VersionRepository {
	return &VersionRepository{db: db, now: time.Now}
}

// ShotVersions returns the prompt versions of the Shot, newest first (ErrNotFound for an unknown Shot).
func (r *VersionRepository) ShotVersions(ctx context.Context, shotID string) ([]domain.VersionEntry, error) {
	if err := exists(ctx, r.db.Reader(), `SELECT 1 FROM shot WHERE id = ?`, shotID); err != nil {
		return nil, err
	}
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT version_number, shot_spec_snapshot, created_at FROM prompt_version WHERE shot_id = ? ORDER BY version_number DESC`, shotID)
	if err != nil {
		return nil, fmt.Errorf("list prompt versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.VersionEntry
	for rows.Next() {
		var (
			e            domain.VersionEntry
			snapshot, at string
		)
		if err := rows.Scan(&e.Number, &snapshot, &at); err != nil {
			return nil, err
		}
		var probe struct {
			Objective string `json:"shot_objective"`
		}
		_ = json.Unmarshal([]byte(snapshot), &probe) // a snapshot without a spec has no summary
		e.Summary = probe.Objective
		if e.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// WorldStateVersions returns the World State versions of the Project's trunk (branchID "") or of
// the Branch, newest first. ErrNotFound: unknown Project, or a Branch that is not in the Project.
func (r *VersionRepository) WorldStateVersions(ctx context.Context, projectID, branchID string) ([]domain.VersionEntry, error) {
	if err := scopeExists(ctx, r.db.Reader(), projectID, branchID); err != nil {
		return nil, err
	}
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT version_number, state, created_at FROM world_state_version WHERE project_id = ? AND branch_id IS ? ORDER BY version_number DESC`,
		projectID, nullIfEmpty(branchID))
	if err != nil {
		return nil, fmt.Errorf("list world state versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.VersionEntry
	for rows.Next() {
		var (
			e         domain.VersionEntry
			state, at string
		)
		if err := rows.Scan(&e.Number, &state, &at); err != nil {
			return nil, err
		}
		var w domain.WorldContext
		if err := json.Unmarshal([]byte(state), &w); err == nil {
			e.Summary = fmt.Sprintf("Character %d / Product %d / Location %d", len(w.Characters), len(w.Products), len(w.Locations))
		}
		if e.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// BranchVersions returns the versions of the Branch, newest first (ErrNotFound for an unknown Branch).
func (r *VersionRepository) BranchVersions(ctx context.Context, branchID string) ([]domain.VersionEntry, error) {
	if err := exists(ctx, r.db.Reader(), `SELECT 1 FROM branch WHERE id = ?`, branchID); err != nil {
		return nil, err
	}
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT version_number, name, created_at FROM branch_version WHERE branch_id = ? ORDER BY version_number DESC`, branchID)
	if err != nil {
		return nil, fmt.Errorf("list branch versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.VersionEntry
	for rows.Next() {
		var (
			e  domain.VersionEntry
			at string
		)
		if err := rows.Scan(&e.Number, &e.Summary, &at); err != nil {
			return nil, err
		}
		if e.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// exists returns ErrNotFound when the query (SELECT 1 ... WHERE id = ?) finds no row.
func exists(ctx context.Context, q queryer, query string, args ...any) error {
	var one int
	err := q.QueryRowContext(ctx, query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check existence: %w", err)
	}
	return nil
}

// scopeExists checks the Project and, when branchID is set, that the Branch belongs to it.
func scopeExists(ctx context.Context, q queryer, projectID, branchID string) error {
	if err := exists(ctx, q, `SELECT 1 FROM project WHERE id = ?`, projectID); err != nil {
		return err
	}
	if branchID == "" {
		return nil
	}
	return exists(ctx, q, `SELECT 1 FROM branch WHERE id = ? AND project_id = ?`, branchID, projectID)
}

// RestoreShotVersion makes the Canonical Shot Spec of prompt version number the spec of the Shot
// again, as a new prompt version (the Takes of the old version stay with it) and returns that
// version. ErrNotFound: unknown Shot or version; ErrConflict: the Shot is being generated or repaired;
// *LockedParametersError (Is ErrLocked): the restore would change a LOCKed part of the spec.
func (r *VersionRepository) RestoreShotVersion(ctx context.Context, shotID string, number int) (domain.PromptVersion, error) {
	var pv domain.PromptVersion
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, state, err := readShotSpec(ctx, tx, shotID)
		if err != nil {
			return err
		}
		if state == domain.ShotGenerating || state == domain.ShotRepairing {
			return ErrConflict
		}
		var snapshot string
		err = tx.QueryRowContext(ctx, `SELECT shot_spec_snapshot FROM prompt_version WHERE shot_id = ? AND version_number = ?`, shotID, number).Scan(&snapshot)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		restored, ok := (domain.Shot{ShotSpec: json.RawMessage(snapshot)}).CanonicalSpec()
		if !ok {
			return fmt.Errorf("prompt version %d holds no Canonical Shot Spec", number)
		}
		if old, ok := (domain.Shot{ShotSpec: json.RawMessage(current)}).CanonicalSpec(); ok {
			locks, err := readParameterLocks(ctx, tx, `target_type = ? AND target_id = ?`, domain.LockTargetShot, shotID)
			if err != nil {
				return err
			}
			if hit := domain.LockedChanges(locks, old, restored); len(hit) > 0 {
				return &LockedParametersError{Parameters: hit}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, snapshot, shotID); err != nil {
			return err
		}
		var next int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM prompt_version WHERE shot_id = ?`, shotID).Scan(&next); err != nil {
			return err
		}
		pv = domain.PromptVersion{ID: domain.NewID(), ShotID: shotID, VersionNumber: next, SpecSnapshot: json.RawMessage(snapshot)}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot, created_at) VALUES (?, ?, ?, ?, ?)`,
			pv.ID, shotID, next, snapshot, formatTime(r.now()))
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrLocked) {
		return domain.PromptVersion{}, err
	}
	if err != nil {
		return domain.PromptVersion{}, fmt.Errorf("restore shot version: %w", err)
	}
	return pv, nil
}

// RestoreWorldStateVersion restores the World State of the trunk (branchID "") or of the Branch
// to version number and returns the number of the version that records the restore.
//
// The trunk and every Branch keep their state in their own Character / Product / Location rows, so
// the text fields of the entities the version knows are written back (entities added since are
// left alone), the Camera and Story come from the version, and the result is appended as a new
// version of that series.
// ErrNotFound: unknown Project, Branch or version; ErrLocked: the restore would change a LOCKed field.
func (r *VersionRepository) RestoreWorldStateVersion(ctx context.Context, projectID, branchID string, number int) (int, error) {
	var newNumber int
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := scopeExists(ctx, tx, projectID, branchID); err != nil {
			return err
		}
		var state string
		err := tx.QueryRowContext(ctx,
			`SELECT state FROM world_state_version WHERE project_id = ? AND branch_id IS ? AND version_number = ?`,
			projectID, nullIfEmpty(branchID), number).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		newNumber, err = r.restoreWorld(ctx, tx, projectID, branchID, state)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLocked) {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("restore world state version: %w", err)
	}
	return newNumber, nil
}

// restoreWorld writes the version's entity fields back into the rows of the trunk (or of the
// Branch), checks LOCK on the data being written, and appends the resulting World State to that
// series.
func (r *VersionRepository) restoreWorld(ctx context.Context, tx *sql.Tx, projectID, branchID, state string) (int, error) {
	var snapshot domain.WorldContext
	if err := json.Unmarshal([]byte(state), &snapshot); err != nil {
		return 0, fmt.Errorf("decode world state version: %w", err)
	}
	before, err := readWorld(ctx, tx, projectID, branchID)
	if err != nil {
		return 0, err
	}
	locks, err := readLocks(ctx, tx, projectID)
	if err != nil {
		return 0, err
	}
	type entity struct {
		target domain.WorldTarget
		id     string
	}
	var entities []entity
	for _, c := range snapshot.Characters {
		entities = append(entities, entity{domain.TargetCharacter, c.ID})
	}
	for _, p := range snapshot.Products {
		entities = append(entities, entity{domain.TargetProduct, p.ID})
	}
	for _, l := range snapshot.Locations {
		entities = append(entities, entity{domain.TargetLocation, l.ID})
	}
	for _, e := range entities {
		for _, field := range domain.EditableFields(e.target) {
			old, present := before.Current(e.target, e.id, field)
			if !present {
				break // deleted since: nothing to write back
			}
			want, _ := snapshot.Current(e.target, e.id, field)
			if domain.SameValue(old, want) {
				continue
			}
			if locks.Locked(e.target, e.id, field) {
				return 0, ErrLocked
			}
			var text string
			if len(want) > 0 {
				if err := json.Unmarshal(want, &text); err != nil {
					return 0, fmt.Errorf("decode %s.%s: %w", e.target, field, err)
				}
			}
			if err := setField(ctx, tx, worldTables[e.target], e.id, field, text); err != nil {
				return 0, err
			}
		}
	}
	camStory := before
	for _, target := range []domain.WorldTarget{domain.TargetCamera, domain.TargetStory} {
		for _, field := range domain.LockableFields(target) {
			old, _ := before.Current(target, "", field)
			want, _ := snapshot.Current(target, "", field)
			if domain.SameValue(old, want) {
				continue
			}
			if locks.Locked(target, "", field) {
				return 0, ErrLocked
			}
			if err := camStory.SetSingleton(target, field, want); err != nil {
				return 0, err
			}
		}
	}
	after, err := readWorld(ctx, tx, projectID, branchID)
	if err != nil {
		return 0, err
	}
	after.Camera, after.Story = camStory.Camera, camStory.Story
	wr := &WorldRepository{db: r.db, now: r.now}
	_, number, err := wr.appendVersion(ctx, tx, projectID, branchID, after)
	return number, err
}

// RestoreBranchVersion makes the name and World State reference of branch version number those of
// the Branch again, as a new branch version, and returns its number. ErrNotFound: unknown Branch or version.
func (r *VersionRepository) RestoreBranchVersion(ctx context.Context, branchID string, number int) (int, error) {
	var next int
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var (
			name string
			wsv  sql.NullString
		)
		err := tx.QueryRowContext(ctx, `SELECT name, world_state_version_id FROM branch_version WHERE branch_id = ? AND version_number = ?`, branchID, number).Scan(&name, &wsv)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE branch SET name = ?, world_state_version_id = ? WHERE id = ?`, name, wsv, branchID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM branch_version WHERE branch_id = ?`, branchID).Scan(&next); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO branch_version (id, branch_id, version_number, name, world_state_version_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			domain.NewID(), branchID, next, name, wsv, formatTime(r.now()))
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("restore branch version: %w", err)
	}
	return next, nil
}

// BranchProject returns the Project the Branch belongs to (ErrNotFound for an unknown Branch).
func (r *VersionRepository) BranchProject(ctx context.Context, branchID string) (string, error) {
	var projectID string
	err := r.db.Reader().QueryRowContext(ctx, `SELECT project_id FROM branch WHERE id = ?`, branchID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get branch: %w", err)
	}
	return projectID, nil
}

// ShotProject returns the Project the Shot belongs to (ErrNotFound for an unknown Shot).
func (r *VersionRepository) ShotProject(ctx context.Context, shotID string) (string, error) {
	var projectID string
	err := r.db.Reader().QueryRowContext(ctx, `SELECT sc.project_id FROM shot sh JOIN scene sc ON sc.id = sh.scene_id WHERE sh.id = ?`, shotID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get shot project: %w", err)
	}
	return projectID, nil
}
