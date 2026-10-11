package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ErrLocked means the write would change a field the user has LOCKed (HTTP 423).
var ErrLocked = errors.New("repository: field is locked")

// WorldRepository persists the World State (spec/spec.md §9): the Character, Product and
// Location rows, the appended world_state_version snapshots and the state_diff of each completed
// Shot (spec/architecture/er.md §2.2). All writes go through DB.Write.
type WorldRepository struct {
	db  *DB
	now func() time.Time
}

// NewWorldRepository returns a WorldRepository backed by db.
func NewWorldRepository(db *DB) *WorldRepository {
	return &WorldRepository{db: db, now: time.Now}
}

// queryer is what *sql.DB and *sql.Tx share.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// worldTables maps an entity target to its table. The names are constants, never user input.
var worldTables = map[domain.WorldTarget]string{
	domain.TargetCharacter: "character",
	domain.TargetProduct:   "product",
	domain.TargetLocation:  "location",
}

// World returns the current World State of the Project, or of its Branch when branchID is set ("" is
// the trunk). It returns ErrNotFound when the Project, or the Branch in it, does not exist.
func (r *WorldRepository) World(ctx context.Context, projectID, branchID string) (domain.WorldContext, error) {
	db := r.db.Reader()
	var one int
	if err := db.QueryRowContext(ctx, `SELECT 1 FROM project WHERE id = ?`, projectID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return domain.WorldContext{}, ErrNotFound
	} else if err != nil {
		return domain.WorldContext{}, fmt.Errorf("get project: %w", err)
	}
	if err := branchExists(ctx, db, projectID, branchID); err != nil {
		return domain.WorldContext{}, err
	}
	return readWorld(ctx, db, projectID, branchID)
}

// branchExists returns ErrNotFound when branchID is not a Branch of the Project. "" is the trunk.
func branchExists(ctx context.Context, q queryer, projectID, branchID string) error {
	if branchID == "" {
		return nil
	}
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM branch WHERE id = ? AND project_id = ?`, branchID, projectID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get branch: %w", err)
	}
	return nil
}

// Locks returns the fields of the Project that are LOCKed: the ones the user cannot edit either.
func (r *WorldRepository) Locks(ctx context.Context, projectID string) (domain.LockSet, error) {
	return readLocks(ctx, r.db.Reader(), projectID)
}

// AgentLocks returns the fields of the Project the State Extractor must not change: the LOCKed ones
// and the ones the user set MANUAL (the Story parameter can be MANUAL, spec/spec.md §6).
func (r *WorldRepository) AgentLocks(ctx context.Context, projectID string) (domain.LockSet, error) {
	return readLockRows(ctx, r.db.Reader(), projectID, domain.LockMode.KeepsTheAgentsOut)
}

// readWorld reads the entity rows and the Camera and Story of the latest world_state_version of the
// trunk, or of the Branch when branchID is set. `branch_id IS ?` matches NULL (the trunk) for "".
func readWorld(ctx context.Context, q queryer, projectID, branchID string) (domain.WorldContext, error) {
	var w domain.WorldContext
	var err error
	w.Characters, err = queryAll(ctx, q, func(row rowScanner) (domain.WorldCharacter, error) {
		var c domain.WorldCharacter
		return c, row.Scan(&c.ID, &c.Name, &c.Hairstyle, &c.Wardrobe, &c.Voice, &c.Personality,
			&c.Emotion, &c.Position, &c.CurrentAction, &c.FaceReferenceAssetID, &c.BodyReferenceAssetID, &c.SeriesFaceReferenceAssetID)
	}, `SELECT c.id, c.name, COALESCE(c.hairstyle, ''), COALESCE(c.wardrobe, ''), COALESCE(c.voice, ''), COALESCE(c.personality, ''),
		        COALESCE(c.emotion, ''), COALESCE(c.position, ''), COALESCE(c.current_action, ''),
		        COALESCE(c.face_reference_asset_id, ''), COALESCE(c.body_reference_asset_id, ''),
		        COALESCE(o.face_reference_asset_id, '')
		 FROM character c LEFT JOIN character o ON o.id = c.origin_id
		 WHERE c.project_id = ? AND c.branch_id IS ? ORDER BY c.name, c.id`, projectID, nullIfEmpty(branchID))
	if err != nil {
		return w, fmt.Errorf("list characters: %w", err)
	}
	w.Products, err = queryAll(ctx, q, func(row rowScanner) (domain.WorldProduct, error) {
		var p domain.WorldProduct
		return p, row.Scan(&p.ID, &p.Name, &p.Material, &p.Orientation, &p.Condition, &p.GeometryReferenceAssetID, &p.LabelReferenceAssetID)
	}, `SELECT id, name, COALESCE(material, ''), COALESCE(orientation, ''), COALESCE(condition, ''),
		        COALESCE(geometry_reference_asset_id, ''), COALESCE(label_reference_asset_id, '')
		 FROM product WHERE project_id = ? AND branch_id IS ? ORDER BY name, id`, projectID, nullIfEmpty(branchID))
	if err != nil {
		return w, fmt.Errorf("list products: %w", err)
	}
	w.Locations, err = queryAll(ctx, q, func(row rowScanner) (domain.WorldLocation, error) {
		var l domain.WorldLocation
		return l, row.Scan(&l.ID, &l.Name, &l.Time, &l.LightingState, &l.Weather, &l.LayoutReferenceAssetID)
	}, `SELECT id, name, COALESCE(time, ''), COALESCE(lighting_state, ''), COALESCE(weather, ''), COALESCE(layout_reference_asset_id, '')
		 FROM location WHERE project_id = ? AND branch_id IS ? ORDER BY name, id`, projectID, nullIfEmpty(branchID))
	if err != nil {
		return w, fmt.Errorf("list locations: %w", err)
	}

	var state string
	err = q.QueryRowContext(ctx,
		`SELECT version_number, state FROM world_state_version WHERE project_id = ? AND branch_id IS ? ORDER BY version_number DESC LIMIT 1`,
		projectID, nullIfEmpty(branchID)).
		Scan(&w.Version, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return w, nil
	}
	if err != nil {
		return w, fmt.Errorf("get latest world state version: %w", err)
	}
	var snapshot domain.WorldContext
	if err := json.Unmarshal([]byte(state), &snapshot); err != nil {
		return w, fmt.Errorf("decode world state version %d: %w", w.Version, err)
	}
	w.Camera, w.Story = snapshot.Camera, snapshot.Story
	return w, nil
}

// readLocks reads the lock rows in mode 'lock'. The singleton Camera and Story are locked regardless of target_id.
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

// appendVersion appends a snapshot of the World State as the next world_state_version of the trunk
// (or of the Branch) and returns its ID and number. Versions are never updated or deleted.
func (r *WorldRepository) appendVersion(ctx context.Context, tx *sql.Tx, projectID, branchID string, w domain.WorldContext) (string, int, error) {
	state, err := json.Marshal(w)
	if err != nil {
		return "", 0, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM world_state_version WHERE project_id = ? AND branch_id IS ?`,
		projectID, nullIfEmpty(branchID)).Scan(&next); err != nil {
		return "", 0, err
	}
	id := domain.NewID()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO world_state_version (id, project_id, branch_id, version_number, state, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, projectID, nullIfEmpty(branchID), next, string(state), FormatTime(r.now())); err != nil {
		return "", 0, err
	}
	return id, next, nil
}

// UpdateEntity sets fields of a Character, Product or Location of the Project (F-09) and appends
// the resulting World State as a new version of the trunk, or of the Branch when branchID is set.
// fields maps an editable field name (domain.EditableFields) to its new text; all of them are
// written or none. It returns ErrNotFound when the entity is not in that trunk or Branch and
// ErrLocked when any of the fields is LOCKed.
func (r *WorldRepository) UpdateEntity(ctx context.Context, projectID, branchID string, target domain.WorldTarget, id string, fields map[string]string) error {
	table, ok := worldTables[target]
	if !ok {
		return fmt.Errorf("update %s: not an entity target", target)
	}
	for name := range fields {
		if !domain.IsEditable(target, name) {
			return fmt.Errorf("update %s: field %q is not editable", target, name)
		}
	}
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ? AND project_id = ? AND branch_id IS ?`, id, projectID, nullIfEmpty(branchID)).Scan(&one) //nolint:gosec // table is from worldTables
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		locks, err := readLocks(ctx, tx, projectID)
		if err != nil {
			return err
		}
		for name := range fields {
			if locks.Locked(target, id, name) {
				return ErrLocked
			}
		}
		for name, value := range fields {
			if err := setField(ctx, tx, table, id, name, value); err != nil {
				return err
			}
		}
		w, err := readWorld(ctx, tx, projectID, branchID)
		if err != nil {
			return err
		}
		// The Camera and Story carry over from the previous version.
		_, _, err = r.appendVersion(ctx, tx, projectID, branchID, w)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLocked) {
		return err
	}
	if err != nil {
		return fmt.Errorf("update %s: %w", target, err)
	}
	return nil
}

// setField writes one text column. name has been checked against the editable or extractable fields.
func setField(ctx context.Context, tx *sql.Tx, table, id, name, value string) error {
	_, err := tx.ExecContext(ctx, `UPDATE `+table+` SET `+name+` = ? WHERE id = ?`, nullIfEmpty(value), id) //nolint:gosec // table is from worldTables and name is a domain-checked field; only the value is user input
	return err
}

// ApplyStateDiff applies the State Diff of a completed Shot and appends the resulting World State
// as a new version, in one transaction (F-10). branchID is the Branch the Shot's Scene belongs to
// ("" for the trunk): the diff changes that Branch's own entities and version series only. Changes
// that hit a LOCKed field (or a MANUAL one: the Story the user specified), a missing Character/Product or a field the State Extractor may not
// change are left out, whatever the caller checked before: the LOCK is judged here, on the data
// being written. Each change's From is filled with the value it replaced. It returns the changes
// that were applied and the number of the new version. A diff with no applicable change still
// appends a version, so every completed Shot has the World State it left behind.
func (r *WorldRepository) ApplyStateDiff(ctx context.Context, projectID, branchID, shotID, takeID string, diff domain.StateDiff) (domain.StateDiff, int, error) {
	applied := domain.StateDiff{Changes: []domain.StateChange{}}
	var number int
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		before, err := readWorld(ctx, tx, projectID, branchID)
		if err != nil {
			return err
		}
		locks, err := readLockRows(ctx, tx, projectID, domain.LockMode.KeepsTheAgentsOut)
		if err != nil {
			return err
		}
		camStory := before // Camera and Story are edited on this copy
		for _, c := range diff.Changes {
			if !domain.IsExtractable(c.Target, c.Field) || locks.Locked(c.Target, c.ID, c.Field) ||
				domain.ValidateFieldValue(c.Target, c.Field, c.To) != nil {
				continue
			}
			old, exists := before.Current(c.Target, c.ID, c.Field)
			if !exists {
				continue
			}
			c.From = old
			if c.Target.IsSingleton() {
				if err := camStory.SetSingleton(c.Target, c.Field, c.To); err != nil {
					return err
				}
			} else {
				var text string
				if err := json.Unmarshal(c.To, &text); err != nil {
					continue
				}
				if err := setField(ctx, tx, worldTables[c.Target], c.ID, c.Field, text); err != nil {
					return err
				}
			}
			applied.Changes = append(applied.Changes, c)
		}
		after, err := readWorld(ctx, tx, projectID, branchID)
		if err != nil {
			return err
		}
		after.Camera, after.Story = camStory.Camera, camStory.Story
		versionID, n, err := r.appendVersion(ctx, tx, projectID, branchID, after)
		if err != nil {
			return err
		}
		number = n
		raw, err := json.Marshal(applied)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO state_diff (id, shot_id, take_id, diff, world_state_version_id, applied_at) VALUES (?, ?, ?, ?, ?, ?)`,
			domain.NewID(), shotID, takeID, string(raw), versionID, FormatTime(r.now()))
		return err
	})
	if err != nil {
		return domain.StateDiff{}, 0, fmt.Errorf("apply state diff: %w", err)
	}
	return applied, number, nil
}

// ensureWorldEntity adds the Character or Product a Reference names to the World State (of the
// trunk, or of the Branch) when it has none of that name, so the State Extractor can track it
// (F-09). Locations come from the Scenes (PlanRepository) and other kinds are not World State
// entities.
func ensureWorldEntity(ctx context.Context, tx *sql.Tx, projectID, branchID string, ref domain.Reference) error {
	var table string
	switch ref.Kind {
	case domain.RefCharacter:
		table = worldTables[domain.TargetCharacter]
	case domain.RefProduct:
		table = worldTables[domain.TargetProduct]
	default:
		return nil
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return nil
	}
	// "Mio" and "mio " name the same entity (domain.SameReferenceName): SQLite's = and NOCASE would
	// tell them apart (and only fold ASCII), so the names are compared here.
	existing, err := queryAll(ctx, tx, scanString, `SELECT name FROM `+table+` WHERE project_id = ? AND branch_id IS ?`, projectID, nullIfEmpty(branchID)) //nolint:gosec // table is from worldTables
	if err != nil {
		return err
	}
	if slices.ContainsFunc(existing, func(n string) bool { return domain.SameReferenceName(n, name) }) {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+table+` (id, project_id, branch_id, name) VALUES (?, ?, ?, ?)`, domain.NewID(), projectID, nullIfEmpty(branchID), name) //nolint:gosec // table is from worldTables
	return err
}

// SetFieldLock sets an attribute of the World State to AUTO or LOCK (a lock row of the target;
// the singleton Camera and Story use the Project's ID as target_id). The attribute is not
// checked here (see domain.IsLockable). An unknown Project, or an entity that is not in the
// trunk (or the Branch when branchID is set), is ErrNotFound.
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

// AssignReference sets the image of a reference slot of a Character, Product or Location (the
// asset of a Reference Library entry; "" clears the slot) and appends the resulting World State
// as a new version of the trunk (or of the Branch). The entity is ErrNotFound when it is not in
// that trunk or Branch and the write is ErrLocked when the attribute the slot belongs to
// (slot.Lock) is LOCKed.
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
