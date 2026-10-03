package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
       sh.shot_spec, sh.state, COALESCE(sh.selected_take_id, ''), s.project_id, COALESCE(s.branch_id, ''), s.order_index, s.title, COALESCE(s.time, ''), COALESCE(s.objective, '')
FROM shot sh JOIN scene s ON s.id = sh.scene_id`

func scanShotRef(row rowScanner) (ShotRef, error) {
	var (
		r    ShotRef
		spec string
	)
	sh := &r.Shot
	if err := row.Scan(&sh.ID, &sh.SceneID, &sh.OrderIndex, &sh.Objective, &sh.Duration, &spec, &sh.State, &sh.SelectedTakeID,
		&r.ProjectID, &r.Scene.BranchID, &r.Scene.OrderIndex, &r.Scene.Title, &r.Scene.Time, &r.Scene.Objective); err != nil {
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

// ListProjectShots returns every Shot of the trunk of the Project in story order (Scene, then
// Shot). The Shots of a Branch are read with StoryShots.
func (r *GenerationRepository) ListProjectShots(ctx context.Context, projectID string) ([]ShotRef, error) {
	rows, err := r.db.Reader().QueryContext(ctx, shotRefSelect+` WHERE s.project_id = ? AND s.branch_id IS NULL ORDER BY s.order_index, sh.order_index`, projectID)
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

// StartShot moves the Shot to GENERATING unless it already is (or is being repaired). It reports
// whether this call made the move, so that two requests never generate the same Shot at once.
func (r *GenerationRepository) StartShot(ctx context.Context, shotID string) (bool, error) {
	return r.claimShot(ctx, shotID, domain.ShotGenerating)
}

// StartRepair moves the Shot to REPAIRING unless it is being generated or repaired. It reports
// whether this call made the move, so that a Shot is never repaired twice at once.
func (r *GenerationRepository) StartRepair(ctx context.Context, shotID string) (bool, error) {
	return r.claimShot(ctx, shotID, domain.ShotRepairing)
}

func (r *GenerationRepository) claimShot(ctx context.Context, shotID string, state domain.ShotState) (bool, error) {
	var started bool
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE shot SET state = ? WHERE id = ? AND state NOT IN (?, ?)`,
			state, shotID, domain.ShotGenerating, domain.ShotRepairing)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		started = n > 0
		return err
	})
	if err != nil {
		return false, fmt.Errorf("claim shot: %w", err)
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

// ResetStaleShots puts Shots that are GENERATING, REPAIRING or JUDGING back to PLANNING. It is called
// at start-up, before any job runs: after an abnormal termination no generation, repair or judging is
// in flight. A Shot with a queued generate or repair job keeps its state: that job did not start
// before the restart and will run, and releasing the Shot would let a second one be queued for it (a
// double paid generation). The generate and repair jobs that were running are failed afterwards (they
// are not resumable), so their Shots are released here. A JUDGING Shot also keeps its state while a
// judge job is running, because the queue runs a judge job again (it is resumable) or fails it.
//
// It MUST run twice: before the job queue resolves the running jobs (Queue.Recover) and after. The
// second run releases the Shots whose judge job the queue failed (attempts used up, no handler) or
// whose queued job it failed for lack of a handler: no handler of theirs runs to release them.
func (r *GenerationRepository) ResetStaleShots(ctx context.Context) (int, error) {
	var n int64
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE shot SET state = ?
			  WHERE (state IN (?, ?) AND NOT EXISTS (SELECT 1 FROM job WHERE kind IN (?, ?) AND status = ? AND json_extract(payload, '$.shot_id') = shot.id))
			     OR (state = ? AND NOT EXISTS (SELECT 1 FROM job WHERE kind = ? AND status IN (?, ?) AND json_extract(payload, '$.shot_id') = shot.id))`,
			domain.ShotPlanning,
			domain.ShotGenerating, domain.ShotRepairing, domain.JobGenerate, domain.JobRepair, domain.JobQueued,
			domain.ShotJudging, domain.JobJudge, domain.JobQueued, domain.JobRunning)
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
// stored in prompt_version.model_adapter_output; noteRefs (the note versions the Prompt Planner
// was given, nil for none) in prompt_version.note_refs.
func (r *GenerationRepository) SavePromptedSpec(ctx context.Context, shotID string, spec domain.ShotSpec, adapterOutput json.RawMessage, noteRefs []domain.NoteRef) (domain.PromptVersion, error) {
	return r.savePromptVersion(ctx, shotID, spec, adapterOutput, noteRefs, true)
}

// SaveCandidateSpec appends a prompt_version for a Prompt Candidate like SavePromptedSpec does, but
// leaves shot.shot_spec as it is: the Candidates of a Shot are alternatives, and only the one the
// Judge picks becomes the spec of the Shot (AdoptSpec).
func (r *GenerationRepository) SaveCandidateSpec(ctx context.Context, shotID string, spec domain.ShotSpec, adapterOutput json.RawMessage, noteRefs []domain.NoteRef) (domain.PromptVersion, error) {
	return r.savePromptVersion(ctx, shotID, spec, adapterOutput, noteRefs, false)
}

func (r *GenerationRepository) savePromptVersion(ctx context.Context, shotID string, spec domain.ShotSpec, adapterOutput json.RawMessage, noteRefs []domain.NoteRef, adopt bool) (domain.PromptVersion, error) {
	var pv domain.PromptVersion
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, _, err := readShotSpec(ctx, tx, shotID)
		if err != nil {
			return err
		}
		pv, err = r.writeSpec(ctx, tx, shotID, current, spec, adapterOutput, noteRefs, adopt)
		return err
	})
	if err != nil {
		return domain.PromptVersion{}, fmt.Errorf("save prompted spec: %w", err)
	}
	return pv, nil
}

// AdoptSpec merges the Canonical Shot Spec of a Prompt Candidate into shot.shot_spec (keeping the
// other keys), so that the Shot carries the spec of the Take the Judge picked.
func (r *GenerationRepository) AdoptSpec(ctx context.Context, shotID string, spec domain.ShotSpec) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT shot_spec FROM shot WHERE id = ?`, shotID).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		merged, err := domain.MergeShotSpec(json.RawMessage(current), spec)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(merged), shotID)
		return err
	})
	if err != nil {
		return fmt.Errorf("adopt spec: %w", err)
	}
	return nil
}

// MarkAIPick makes the Take the AI pick of its Shot (take.status = ai_pick). The AI pick of the
// Shot before it goes back to candidate; a Take the user selected or rejected is left alone.
// It returns ErrNotFound when the Take does not belong to the Shot.
func (r *GenerationRepository) MarkAIPick(ctx context.Context, shotID, takeID string) error {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE take SET status = ? WHERE status = ? AND prompt_version_id IN (SELECT id FROM prompt_version WHERE shot_id = ?)`,
			domain.TakeCandidate, domain.TakeAIPick, shotID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE take SET status = ? WHERE id = ? AND status IN (?, ?) AND prompt_version_id IN (SELECT id FROM prompt_version WHERE shot_id = ?)`,
			domain.TakeAIPick, takeID, domain.TakeCandidate, domain.TakeAIPick, shotID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("mark ai pick: %w", err)
	}
	return nil
}

// SaveEditedSpec stores a Canonical Shot Spec the user edited by hand (F-06, DIRECTOR MODE) as a
// new prompt_version. LOCK is judged on the data being written: it returns ErrLocked (wrapping the
// names of the LOCKed parameters the edit touches) and writes nothing when a LOCKed part or, with
// Prompt LOCKed, any part of the current spec would change; ErrConflict while the Shot is being
// generated or repaired (the Repair saves its own spec at its end and would overwrite the edit);
// ErrNotFound for an unknown Shot. A Shot without a spec has nothing to be LOCKed.
func (r *GenerationRepository) SaveEditedSpec(ctx context.Context, shotID string, spec domain.ShotSpec) (domain.PromptVersion, error) {
	var pv domain.PromptVersion
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, state, err := readShotSpec(ctx, tx, shotID)
		if err != nil {
			return err
		}
		if !state.CanEditSpec() {
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
		pv, err = r.writeSpec(ctx, tx, shotID, current, spec, nil, nil, true)
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

// writeSpec appends a prompt_version whose snapshot is the spec merged into the shot_spec JSON
// current, recording the note versions the spec was planned with. adopt also stores the merged spec
// in shot.shot_spec.
func (r *GenerationRepository) writeSpec(ctx context.Context, tx *sql.Tx, shotID, current string, spec domain.ShotSpec, adapterOutput json.RawMessage, noteRefs []domain.NoteRef, adopt bool) (domain.PromptVersion, error) {
	merged, err := domain.MergeShotSpec(json.RawMessage(current), spec)
	if err != nil {
		return domain.PromptVersion{}, err
	}
	if adopt {
		if _, err := tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(merged), shotID); err != nil {
			return domain.PromptVersion{}, err
		}
	}
	if noteRefs == nil {
		noteRefs = []domain.NoteRef{}
	}
	refs, err := json.Marshal(noteRefs)
	if err != nil {
		return domain.PromptVersion{}, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM prompt_version WHERE shot_id = ?`, shotID).Scan(&next); err != nil {
		return domain.PromptVersion{}, err
	}
	pv := domain.PromptVersion{ID: domain.NewID(), ShotID: shotID, VersionNumber: next, SpecSnapshot: merged, AdapterOutput: adapterOutput, NoteRefs: noteRefs}
	var output any
	if len(adapterOutput) > 0 {
		output = string(adapterOutput)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot, model_adapter_output, note_refs, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		pv.ID, shotID, next, string(merged), output, string(refs), formatTime(r.now()))
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

// AddCost appends an entry to the cost ledger for a paid output that no Take or Pack row holds:
// the provider has charged, so the spend stays on the ledger.
func (r *GenerationRepository) AddCost(ctx context.Context, c domain.CostEntry) error {
	now := formatTime(r.now())
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error { return insertCost(ctx, tx, c, now) })
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

// NewReferencePack is the Reference Pack of a Shot with the image assets. Their cost is on the
// ledger already (AddCost, written when each image was paid for). References
// may include images that already exist (pinned from the Reference Library, or the images a World
// State reference slot holds): only the images in Assets are new.
type NewReferencePack struct {
	ProjectID  string
	ShotID     string
	References []domain.Reference
	Assets     []domain.Asset // the new image assets (type reference)
}

// SaveReferencePack replaces the Reference Pack of the Shot: the assets are inserted,
// the new images join the Project's Reference Library (spec/spec.md §10.1; the Keyframe stays
// with its Shot), the Characters and Products it names join the World State (when not already
// there) and the "references" key of shot.shot_spec is rewritten, in one transaction.
func (r *GenerationRepository) SaveReferencePack(ctx context.Context, p NewReferencePack) error {
	now := formatTime(r.now())
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var current, branchID string
		if err := tx.QueryRowContext(ctx,
			`SELECT sh.shot_spec, COALESCE(s.branch_id, '') FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE sh.id = ?`, p.ShotID).Scan(&current, &branchID); err != nil {
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
		added := make(map[string]bool, len(p.Assets))
		for _, a := range p.Assets {
			added[a.ID] = true
		}
		for _, ref := range p.References {
			if !added[ref.AssetID] || !domain.ValidLibraryKind(ref.Kind) {
				continue
			}
			item := domain.ReferenceItem{
				ID: domain.NewID(), ProjectID: p.ProjectID, AssetID: ref.AssetID, Kind: ref.Kind,
				Name: libraryName(ref), Tags: []string{}, Source: domain.SourceGenerated, SourceShotID: p.ShotID,
			}
			if err := insertReferenceItem(ctx, tx, item, now); err != nil {
				return err
			}
		}
		// Only an image generated for this Pack, under a name the Art Director gave, adds an entity: a
		// pinned library item or a reused reference keeps the name it was saved with (a library item is
		// named after its image, "Mio（全身）", not after a Character), and was ensured when it was made.
		for _, ref := range p.References {
			if !added[ref.AssetID] {
				continue
			}
			if err := ensureWorldEntity(ctx, tx, p.ProjectID, branchID, ref); err != nil {
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

// libraryName is the name a generated reference gets in the Reference Library: the name the Art
// Director gave it, or its kind for a Style / Lighting image; at most domain.MaxReferenceNameRunes.
func libraryName(ref domain.Reference) string {
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		name = ref.Kind.Label()
	}
	if r := []rune(name); len(r) > domain.MaxReferenceNameRunes {
		name = string(r[:domain.MaxReferenceNameRunes])
	}
	return name
}

// WorldContext returns the World State of the trunk of the Project, or of its Branch when branchID
// is set: its Characters, Products and Locations and the Camera and Story of the latest Shot.
func (r *GenerationRepository) WorldContext(ctx context.Context, projectID, branchID string) (domain.WorldContext, error) {
	return readWorld(ctx, r.db.Reader(), projectID, branchID)
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

// ShotOfKeyframe returns the Shot whose Reference Pack holds the image asset (the Shot a Keyframe
// card is about), when the Shot is in the Project. An image that several Packs hold (a pinned one)
// belongs to the first Shot in story order.
func (r *GenerationRepository) ShotOfKeyframe(ctx context.Context, projectID, assetID string) (ShotRef, error) {
	ref, err := scanShotRef(r.db.Reader().QueryRowContext(ctx,
		shotRefSelect+` WHERE s.project_id = ? AND EXISTS (SELECT 1 FROM json_each(sh.shot_spec, '$.references') j WHERE json_extract(j.value, '$.asset_id') = ?)
ORDER BY s.order_index, sh.order_index LIMIT 1`, projectID, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return ShotRef{}, ErrNotFound
	}
	if err != nil {
		return ShotRef{}, fmt.Errorf("get shot of keyframe: %w", err)
	}
	return ref, nil
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
