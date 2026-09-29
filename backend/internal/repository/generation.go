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

// GenerationRepository persists what generation produces: the Canonical Shot Spec of a Shot,
// its prompt versions, Reference Pack, Takes and their cost (spec/architecture/er.md §4).
// All writes go through DB.Write.
type GenerationRepository struct {
	db  *DB
	now func() time.Time
}

// NewGenerationRepository returns a GenerationRepository backed by db.
func NewGenerationRepository(db *DB) *GenerationRepository {
	return &GenerationRepository{db: db, now: time.Now}
}

// ShotRef is a Shot with the Project and Scene it belongs to.
type ShotRef struct {
	Shot      domain.Shot
	ProjectID string
	Scene     domain.Scene
}

const shotRefSelect = `SELECT sh.id, sh.scene_id, sh.order_index, COALESCE(sh.objective, ''), COALESCE(sh.duration_seconds, 0),
       sh.shot_spec, sh.state, COALESCE(sh.selected_take_id, ''), s.project_id, s.order_index, s.title, COALESCE(s.time, ''), COALESCE(s.objective, '')
FROM shot sh JOIN scene s ON s.id = sh.scene_id`

func scanShotRef(row rowScanner) (ShotRef, error) {
	var (
		r    ShotRef
		spec string
	)
	sh := &r.Shot
	if err := row.Scan(&sh.ID, &sh.SceneID, &sh.OrderIndex, &sh.Objective, &sh.Duration, &spec, &sh.State, &sh.SelectedTakeID,
		&r.ProjectID, &r.Scene.OrderIndex, &r.Scene.Title, &r.Scene.Time, &r.Scene.Objective); err != nil {
		return ShotRef{}, err
	}
	sh.ShotSpec = json.RawMessage(spec)
	r.Scene.ID, r.Scene.ProjectID = sh.SceneID, r.ProjectID
	return r, nil
}

// GetShot returns the Shot with its Project or ErrNotFound.
func (r *GenerationRepository) GetShot(ctx context.Context, shotID string) (ShotRef, error) {
	ref, err := scanShotRef(r.db.Reader().QueryRowContext(ctx, shotRefSelect+` WHERE sh.id = ?`, shotID))
	if errors.Is(err, sql.ErrNoRows) {
		return ShotRef{}, ErrNotFound
	}
	if err != nil {
		return ShotRef{}, fmt.Errorf("get shot: %w", err)
	}
	return ref, nil
}

// ListProjectShots returns every Shot of the Project in story order (Scene, then Shot).
func (r *GenerationRepository) ListProjectShots(ctx context.Context, projectID string) ([]ShotRef, error) {
	rows, err := r.db.Reader().QueryContext(ctx, shotRefSelect+` WHERE s.project_id = ? ORDER BY s.order_index, sh.order_index`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project shots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ShotRef
	for rows.Next() {
		ref, err := scanShotRef(rows)
		if err != nil {
			return nil, fmt.Errorf("scan shot: %w", err)
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// StartShot moves the Shot to GENERATING unless it already is. It reports whether this call
// made the move, so that two requests never generate the same Shot at once.
func (r *GenerationRepository) StartShot(ctx context.Context, shotID string) (bool, error) {
	var started bool
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE shot SET state = ? WHERE id = ? AND state <> ?`,
			domain.ShotGenerating, shotID, domain.ShotGenerating)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		started = n > 0
		return err
	})
	if err != nil {
		return false, fmt.Errorf("start shot: %w", err)
	}
	return started, nil
}

// SetShotState sets the sub-state of the Shot. It returns ErrNotFound for an unknown Shot.
func (r *GenerationRepository) SetShotState(ctx context.Context, shotID string, state domain.ShotState) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE shot SET state = ? WHERE id = ?`, state, shotID)
		if err != nil {
			return fmt.Errorf("set shot state: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ResetStaleShots puts Shots that are GENERATING back to PLANNING. It is called at start-up,
// before any job runs: after an abnormal termination no generation is in flight.
func (r *GenerationRepository) ResetStaleShots(ctx context.Context) (int, error) {
	var n int64
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE shot SET state = ? WHERE state = ?`, domain.ShotPlanning, domain.ShotGenerating)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return int(n), err
}

// SavePromptedSpec stores the Canonical Shot Spec in shot.shot_spec (keeping the other keys)
// and appends a prompt_version whose snapshot is the resulting shot_spec. adapterOutput is
// stored in prompt_version.model_adapter_output.
func (r *GenerationRepository) SavePromptedSpec(ctx context.Context, shotID string, spec domain.ShotSpec, adapterOutput json.RawMessage) (domain.PromptVersion, error) {
	var pv domain.PromptVersion
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, _, err := readShotSpec(ctx, tx, shotID)
		if err != nil {
			return err
		}
		pv, err = r.writeSpec(ctx, tx, shotID, current, spec, adapterOutput)
		return err
	})
	if err != nil {
		return domain.PromptVersion{}, fmt.Errorf("save prompted spec: %w", err)
	}
	return pv, nil
}

// SaveEditedSpec stores a Canonical Shot Spec the user edited by hand (F-06, DIRECTOR MODE) as a
// new prompt_version. LOCK is judged on the data being written: it returns ErrLocked (wrapping the
// names of the LOCKed parameters the edit touches) and writes nothing when a LOCKed part or, with
// Prompt LOCKed, any part of the current spec would change; ErrConflict while the Shot is being
// generated; ErrNotFound for an unknown Shot. A Shot without a spec has nothing to be LOCKed.
func (r *GenerationRepository) SaveEditedSpec(ctx context.Context, shotID string, spec domain.ShotSpec) (domain.PromptVersion, error) {
	var pv domain.PromptVersion
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, state, err := readShotSpec(ctx, tx, shotID)
		if err != nil {
			return err
		}
		if state == domain.ShotGenerating {
			return ErrConflict
		}
		if old, ok := (domain.Shot{ShotSpec: json.RawMessage(current)}).CanonicalSpec(); ok {
			locks, err := readParameterLocks(ctx, tx, `target_type = ? AND target_id = ?`, domain.LockTargetShot, shotID)
			if err != nil {
				return err
			}
			if hit := domain.LockedChanges(locks, old, spec); len(hit) > 0 {
				return &LockedParametersError{Parameters: hit}
			}
		}
		pv, err = r.writeSpec(ctx, tx, shotID, current, spec, nil)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) || errors.Is(err, ErrLocked) {
		return domain.PromptVersion{}, err
	}
	if err != nil {
		return domain.PromptVersion{}, fmt.Errorf("save edited spec: %w", err)
	}
	return pv, nil
}

// readShotSpec reads shot.shot_spec and the state of the Shot.
func readShotSpec(ctx context.Context, tx *sql.Tx, shotID string) (string, domain.ShotState, error) {
	var (
		spec  string
		state domain.ShotState
	)
	if err := tx.QueryRowContext(ctx, `SELECT shot_spec, state FROM shot WHERE id = ?`, shotID).Scan(&spec, &state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return spec, state, nil
}

// writeSpec merges the spec into the shot_spec JSON current and appends a prompt_version.
func (r *GenerationRepository) writeSpec(ctx context.Context, tx *sql.Tx, shotID, current string, spec domain.ShotSpec, adapterOutput json.RawMessage) (domain.PromptVersion, error) {
	merged, err := domain.MergeShotSpec(json.RawMessage(current), spec)
	if err != nil {
		return domain.PromptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(merged), shotID); err != nil {
		return domain.PromptVersion{}, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM prompt_version WHERE shot_id = ?`, shotID).Scan(&next); err != nil {
		return domain.PromptVersion{}, err
	}
	pv := domain.PromptVersion{ID: domain.NewID(), ShotID: shotID, VersionNumber: next, SpecSnapshot: merged, AdapterOutput: adapterOutput}
	var output any
	if len(adapterOutput) > 0 {
		output = string(adapterOutput)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot, model_adapter_output, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		pv.ID, shotID, next, string(merged), output, formatTime(r.now()))
	return pv, err
}

// NewTake is a finished generation: the Take row, the asset row of its video file and the
// estimated cost, written together.
type NewTake struct {
	ProjectID       string
	ShotID          string
	PromptVersionID string
	Model           domain.ModelName
	Parameters      json.RawMessage
	Asset           domain.Asset // Type video; ID and FilePath are set by the caller
	EstimatedCost   float64
	ActualCost      *float64 // reported by the vendor; nil when it is not
}

// CreateTake stores a Take (status candidate) with its video asset and cost ledger entry and
// returns the Take ID.
func (r *GenerationRepository) CreateTake(ctx context.Context, nt NewTake) (string, error) {
	takeID := domain.NewID()
	params := string(nt.Parameters)
	if params == "" {
		params = "{}"
	}
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := insertAsset(ctx, tx, nt.Asset, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			takeID, nt.PromptVersionID, nt.Model, params, nt.Asset.FilePath, domain.TakeCandidate, now); err != nil {
			return fmt.Errorf("insert take: %w", err)
		}
		return insertCost(ctx, tx, domain.CostEntry{
			ProjectID: nt.ProjectID, ShotID: nt.ShotID, TakeID: takeID, Provider: nt.Model, EstimatedCost: nt.EstimatedCost, ActualCost: nt.ActualCost,
		}, now)
	})
	if err != nil {
		return "", fmt.Errorf("create take: %w", err)
	}
	return takeID, nil
}

func insertCost(ctx context.Context, tx *sql.Tx, c domain.CostEntry, now string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO cost_ledger_entry (id, project_id, shot_id, take_id, provider, estimated_cost, actual_cost, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		domain.NewID(), c.ProjectID, nullIfEmpty(c.ShotID), nullIfEmpty(c.TakeID), c.Provider, c.EstimatedCost, c.ActualCost, now)
	if err != nil {
		return fmt.Errorf("insert cost: %w", err)
	}
	return nil
}

// NewReferencePack is the Reference Pack of a Shot with the image assets and their cost.
type NewReferencePack struct {
	ProjectID  string
	ShotID     string
	References []domain.Reference
	Assets     []domain.Asset // one per reference, same order (type reference)
	Costs      []domain.CostEntry
}

// SaveReferencePack replaces the Reference Pack of the Shot: the assets and costs are inserted,
// the Characters and Products it names join the World State (when not already there) and the
// "references" key of shot.shot_spec is rewritten, in one transaction.
func (r *GenerationRepository) SaveReferencePack(ctx context.Context, p NewReferencePack) error {
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT shot_spec FROM shot WHERE id = ?`, p.ShotID).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		for _, a := range p.Assets {
			if err := insertAsset(ctx, tx, a, now); err != nil {
				return err
			}
		}
		for _, c := range p.Costs {
			if err := insertCost(ctx, tx, c, now); err != nil {
				return err
			}
		}
		for _, ref := range p.References {
			if err := ensureWorldEntity(ctx, tx, p.ProjectID, ref); err != nil {
				return err
			}
		}
		next, err := domain.WithReferences(json.RawMessage(current), p.References)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(next), p.ShotID)
		return err
	})
	if err != nil {
		return fmt.Errorf("save reference pack: %w", err)
	}
	return nil
}

// WorldContext returns the World State of the Project: its Characters, Products and Locations
// and the Camera and Story of the latest Shot.
func (r *GenerationRepository) WorldContext(ctx context.Context, projectID string) (domain.WorldContext, error) {
	return readWorld(ctx, r.db.Reader(), projectID)
}

// Budget is the money side of a Shot: the ceilings of the Project and the Shot (nil when unset,
// F-22) and what the cost ledger holds so far.
type Budget struct {
	ProjectCeiling, ShotCeiling *float64
	ProjectSpent, ShotSpent     float64
}

// Budget returns the ceilings of the Project and the estimated cost recorded so far for the
// Project and for the Shot.
func (r *GenerationRepository) Budget(ctx context.Context, projectID, shotID string) (Budget, error) {
	var (
		b                   Budget
		projectCap, shotCap sql.NullFloat64
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT p.project_budget_ceiling, p.shot_budget_ceiling,
		        COALESCE((SELECT SUM(COALESCE(actual_cost, estimated_cost)) FROM cost_ledger_entry WHERE project_id = p.id), 0),
		        COALESCE((SELECT SUM(COALESCE(actual_cost, estimated_cost)) FROM cost_ledger_entry WHERE shot_id = ?), 0)
		 FROM project p WHERE p.id = ?`, shotID, projectID).
		Scan(&projectCap, &shotCap, &b.ProjectSpent, &b.ShotSpent)
	if errors.Is(err, sql.ErrNoRows) {
		return Budget{}, ErrNotFound
	}
	if err != nil {
		return Budget{}, fmt.Errorf("get budget: %w", err)
	}
	if projectCap.Valid {
		b.ProjectCeiling = &projectCap.Float64
	}
	if shotCap.Valid {
		b.ShotCeiling = &shotCap.Float64
	}
	return b, nil
}

// ShotLocks returns the AUTO / MANUAL / LOCK setting of the parameters of the Shot.
func (r *GenerationRepository) ShotLocks(ctx context.Context, shotID string) (domain.ParameterLocks, error) {
	return readParameterLocks(ctx, r.db.Reader(), `target_type = ? AND target_id = ?`, domain.LockTargetShot, shotID)
}

// ShotOfTake returns the Shot that the Take belongs to, when the Shot is in the Project
// (ErrNotFound otherwise).
func (r *GenerationRepository) ShotOfTake(ctx context.Context, projectID, takeID string) (ShotRef, error) {
	ref, err := scanShotRef(r.db.Reader().QueryRowContext(ctx,
		shotRefSelect+` WHERE s.project_id = ? AND sh.id = (SELECT pv.shot_id FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id WHERE t.id = ?)`, projectID, takeID))
	if errors.Is(err, sql.ErrNoRows) {
		return ShotRef{}, ErrNotFound
	}
	if err != nil {
		return ShotRef{}, fmt.Errorf("get shot of take: %w", err)
	}
	return ref, nil
}
