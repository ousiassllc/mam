// Package comfywf is the repository of the ComfyUI workflows saved in mam (tables comfy_workflow and
// comfy_workflow_version, spec/architecture/comfy/comfy-workflows.md). It sits beside the per-aggregate repositories of
// the parent package and uses its DB and its error values.
package comfywf

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Workflow is one row of comfy_workflow. UIJSON is empty in the rows List returns.
type Workflow struct {
	ID   string
	Name string
	// Kind is "image" or "video".
	Kind     string
	Endpoint string
	UIJSON   string
	// APIJSON is the workflow in API format; Assignment the JSON the service reads (assignments, output node, ...).
	APIJSON    json.RawMessage
	Assignment json.RawMessage
	Version    int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Version is one row of comfy_workflow_version: what ran as version Version of the workflow.
type Version struct {
	WorkflowID string
	Version    int
	APIJSON    json.RawMessage
	Assignment json.RawMessage
	CreatedAt  time.Time
}

// Repository persists the workflows. All writes go through DB.Write.
type Repository struct{ db *repository.DB }

// New returns a Repository backed by db.
func New(db *repository.DB) *Repository { return &Repository{db: db} }

const (
	summaryColumns = `id, name, kind, endpoint, '', api_json, assignment_json, version, created_at, updated_at`
	fullColumns    = `id, name, kind, endpoint, ui_json, api_json, assignment_json, version, created_at, updated_at`
)

// Create inserts w as version w.Version (1) together with its version row. A name that is taken (ignoring case)
// is repository.ErrConflict.
func (r *Repository) Create(ctx context.Context, w Workflow) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO comfy_workflow (id, name, kind, endpoint, ui_json, api_json, assignment_json, version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			w.ID, w.Name, w.Kind, w.Endpoint, w.UIJSON, string(w.APIJSON), string(w.Assignment), w.Version,
			repository.FormatTime(w.CreatedAt), repository.FormatTime(w.UpdatedAt))
		if err != nil {
			return conflict("insert comfy workflow", err)
		}
		return insertVersion(ctx, tx, w, w.CreatedAt)
	})
}

// Update replaces the row of w.ID and writes the version row of w.Version (a new version when the caller bumped it,
// else the snapshot of the same version is replaced: the caller edits in place only a version that never ran). An
// unknown ID is repository.ErrNotFound, a name that is taken repository.ErrConflict.
func (r *Repository) Update(ctx context.Context, w Workflow) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE comfy_workflow SET name = ?, kind = ?, endpoint = ?, ui_json = ?, api_json = ?, assignment_json = ?, version = ?, updated_at = ?
			 WHERE id = ?`,
			w.Name, w.Kind, w.Endpoint, w.UIJSON, string(w.APIJSON), string(w.Assignment), w.Version, repository.FormatTime(w.UpdatedAt), w.ID)
		if err != nil {
			return conflict("update comfy workflow", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return repository.ErrNotFound
		}
		return upsertVersion(ctx, tx, w)
	})
}

func insertVersion(ctx context.Context, tx *sql.Tx, w Workflow, at time.Time) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO comfy_workflow_version (workflow_id, version, api_json, assignment_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		w.ID, w.Version, string(w.APIJSON), string(w.Assignment), repository.FormatTime(at))
	if err != nil {
		return fmt.Errorf("insert comfy workflow version: %w", err)
	}
	return nil
}

func upsertVersion(ctx context.Context, tx *sql.Tx, w Workflow) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO comfy_workflow_version (workflow_id, version, api_json, assignment_json, created_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (workflow_id, version) DO UPDATE SET api_json = excluded.api_json, assignment_json = excluded.assignment_json`,
		w.ID, w.Version, string(w.APIJSON), string(w.Assignment), repository.FormatTime(w.UpdatedAt))
	if err != nil {
		return fmt.Errorf("write comfy workflow version: %w", err)
	}
	return nil
}

// Delete removes the workflow and its versions. An unknown ID is repository.ErrNotFound.
func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM comfy_workflow WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete comfy workflow: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return repository.ErrNotFound
		}
		return nil
	})
}

// List returns every workflow by name, without the editor JSON.
func (r *Repository) List(ctx context.Context) ([]Workflow, error) {
	rows, err := r.db.Reader().QueryContext(ctx, `SELECT `+summaryColumns+` FROM comfy_workflow ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list comfy workflows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Workflow
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Get returns the workflow with the ID, or repository.ErrNotFound.
func (r *Repository) Get(ctx context.Context, id string) (Workflow, error) {
	w, err := scan(r.db.Reader().QueryRowContext(ctx, `SELECT `+fullColumns+` FROM comfy_workflow WHERE id = ?`, id))
	return w, notFound(err)
}

// GetByName returns the workflow with the name (case-insensitive), or repository.ErrNotFound.
func (r *Repository) GetByName(ctx context.Context, name string) (Workflow, error) {
	w, err := scan(r.db.Reader().QueryRowContext(ctx, `SELECT `+fullColumns+` FROM comfy_workflow WHERE name = ?`, name))
	return w, notFound(err)
}

// GetVersion returns what ran as the version of the workflow, or repository.ErrNotFound.
func (r *Repository) GetVersion(ctx context.Context, id string, version int) (Version, error) {
	v, err := scanVersion(r.db.Reader().QueryRowContext(ctx,
		`SELECT workflow_id, version, api_json, assignment_json, created_at FROM comfy_workflow_version WHERE workflow_id = ? AND version = ?`, id, version))
	return v, notFound(err)
}

// Versions returns every version of every workflow, by workflow and version.
func (r *Repository) Versions(ctx context.Context) ([]Version, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT workflow_id, version, api_json, assignment_json, created_at FROM comfy_workflow_version ORDER BY workflow_id, version`)
	if err != nil {
		return nil, fmt.Errorf("list comfy workflow versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// PinnedModels are the identifiers the Video Model locks of the Shots name (AUTO locks pin nothing).
func (r *Repository) PinnedModels(ctx context.Context) ([]string, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT DISTINCT json_extract(value, '$') FROM lock
		 WHERE parameter_name = 'video_model' AND mode <> 'auto' AND value IS NOT NULL AND json_valid(value) AND json_extract(value, '$') LIKE 'comfy:%'`)
	if err != nil {
		return nil, fmt.Errorf("list pinned workflows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scan(s rowScanner) (Workflow, error) {
	var w Workflow
	var api, assignment, created, updated string
	if err := s.Scan(&w.ID, &w.Name, &w.Kind, &w.Endpoint, &w.UIJSON, &api, &assignment, &w.Version, &created, &updated); err != nil {
		return Workflow{}, err
	}
	w.APIJSON, w.Assignment = json.RawMessage(api), json.RawMessage(assignment)
	var err error
	if w.CreatedAt, err = repository.ParseTime(created); err != nil {
		return Workflow{}, err
	}
	if w.UpdatedAt, err = repository.ParseTime(updated); err != nil {
		return Workflow{}, err
	}
	return w, nil
}

func scanVersion(s rowScanner) (Version, error) {
	var v Version
	var api, assignment, created string
	if err := s.Scan(&v.WorkflowID, &v.Version, &api, &assignment, &created); err != nil {
		return Version{}, err
	}
	v.APIJSON, v.Assignment = json.RawMessage(api), json.RawMessage(assignment)
	var err error
	if v.CreatedAt, err = repository.ParseTime(created); err != nil {
		return Version{}, err
	}
	return v, nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}

// conflict maps a UNIQUE violation (the name) to repository.ErrConflict.
func conflict(what string, err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE {
		return repository.ErrConflict
	}
	return fmt.Errorf("%s: %w", what, err)
}
