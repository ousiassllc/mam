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

// ErrVersionNotFound means the World State version a Branch was told to refer to is not in its own version series.
var ErrVersionNotFound = errors.New("repository: world state version not found")

// BranchRepository persists Branches (F-17, spec/architecture/er.md §4 "Branch"): the Branch Point with the Snapshot of
// the World State taken there, and the Branch's own World State, a copy of the Snapshot never shared with the trunk or
// another Branch. Writes go through DB.Write.
type BranchRepository struct {
	db    *DB
	now   func() time.Time
	world *WorldRepository
}

// NewBranchRepository returns a BranchRepository backed by db.
func NewBranchRepository(db *DB) *BranchRepository {
	return &BranchRepository{db: db, now: time.Now, world: NewWorldRepository(db)}
}

// NewBranch describes a Branch to create at a Shot.
type NewBranch struct {
	ShotID string
	// PointType is scene (the Scene of the Shot), shot (after the Shot) or frame (OffsetSeconds into it).
	PointType     domain.BranchPointType
	OffsetSeconds float64
	Name          string
}

// snapshotSelect is the World State a completed Shot left behind: the version its state_diff points at.
const snapshotSelect = `SELECT v.state FROM state_diff d JOIN world_state_version v ON v.id = d.world_state_version_id`

// snapshotOf returns the World State to snapshot at a Branch Point of the series (trunk or Branch) the Shot lies in: the
// state the Shot left behind for shot and frame, the state the last Shot before the Scene left behind for scene; the
// current state of the series when no Shot has completed there.
func snapshotOf(ctx context.Context, tx *sql.Tx, projectID, seriesBranch string, typ domain.BranchPointType, shotID string, sceneOrder int) (domain.WorldContext, error) {
	var state string
	var err error
	if typ == domain.BranchAtScene {
		err = tx.QueryRowContext(ctx, snapshotSelect+`
			JOIN shot sh ON sh.id = d.shot_id JOIN scene sc ON sc.id = sh.scene_id
			WHERE sc.project_id = ? AND sc.branch_id IS ? AND sc.order_index < ?
			ORDER BY sc.order_index DESC, sh.order_index DESC, d.applied_at DESC, d.rowid DESC LIMIT 1`,
			projectID, nullIfEmpty(seriesBranch), sceneOrder).Scan(&state)
	} else {
		err = tx.QueryRowContext(ctx, snapshotSelect+` WHERE d.shot_id = ? ORDER BY d.applied_at DESC, d.rowid DESC LIMIT 1`, shotID).Scan(&state)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return readWorld(ctx, tx, projectID, seriesBranch)
	}
	if err != nil {
		return domain.WorldContext{}, fmt.Errorf("read snapshot: %w", err)
	}
	var w domain.WorldContext
	if err := json.Unmarshal([]byte(state), &w); err != nil {
		return domain.WorldContext{}, fmt.Errorf("decode snapshot: %w", err)
	}
	return w, nil
}

// Create saves the Branch Point at the Shot, snapshots the World State there and gives the Branch
// its own copy of it (Characters, Products, Locations, their LOCKs and the first world_state_version
// of the Branch), all in one transaction, and appends branch_version 1. It returns ErrNotFound
// when the Shot does not exist.
func (r *BranchRepository) Create(ctx context.Context, in NewBranch) (domain.Branch, error) {
	id := domain.NewID()
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var (
			projectID, sceneID, seriesBranch string
			sceneOrder                       int
		)
		err := tx.QueryRowContext(ctx,
			`SELECT s.project_id, s.id, COALESCE(s.branch_id, ''), s.order_index FROM shot sh JOIN scene s ON s.id = sh.scene_id WHERE sh.id = ?`,
			in.ShotID).Scan(&projectID, &sceneID, &seriesBranch, &sceneOrder)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		snapshot, err := snapshotOf(ctx, tx, projectID, seriesBranch, in.PointType, in.ShotID, sceneOrder)
		if err != nil {
			return err
		}

		var pointScene, pointShot, offset any
		switch in.PointType {
		case domain.BranchAtScene:
			pointScene = sceneID
		case domain.BranchAtFrame:
			pointShot, offset = in.ShotID, in.OffsetSeconds
		default:
			pointShot = in.ShotID
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO branch (id, project_id, branch_point_type, branch_point_scene_id, branch_point_shot_id, branch_point_offset_seconds, name, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, projectID, in.PointType, pointScene, pointShot, offset, in.Name, FormatTime(r.now())); err != nil {
			return err
		}
		if err := copyWorldInto(ctx, tx, projectID, id, snapshot); err != nil {
			return err
		}
		w, err := readWorld(ctx, tx, projectID, id)
		if err != nil {
			return err
		}
		w.Camera, w.Story = snapshot.Camera, snapshot.Story
		versionID, _, err := r.world.appendVersion(ctx, tx, projectID, id, w)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE branch SET world_state_version_id = ? WHERE id = ?`, versionID, id); err != nil {
			return err
		}
		return r.appendBranchVersion(ctx, tx, id, in.Name, versionID)
	})
	if errors.Is(err, ErrNotFound) {
		return domain.Branch{}, err
	}
	if err != nil {
		return domain.Branch{}, fmt.Errorf("create branch: %w", err)
	}
	return r.Get(ctx, id)
}

// copyWorldInto gives the Branch its own Characters, Products and Locations with the values of the
// snapshot, and the LOCKs the snapshot's entities had. A row keeps the origin_id of the row it was
// copied from, so a Branch of a Series Episode is still measured against the canon face.
func copyWorldInto(ctx context.Context, tx *sql.Tx, projectID, branchID string, w domain.WorldContext) error {
	copyLocks := func(target domain.WorldTarget, from, to string) error {
		type lockRow struct {
			parameter, mode string
			value           sql.NullString
		}
		locks, err := queryAll(ctx, tx, func(row rowScanner) (lockRow, error) {
			var l lockRow
			return l, row.Scan(&l.parameter, &l.mode, &l.value)
		}, `SELECT parameter_name, mode, value FROM lock WHERE project_id = ? AND target_type = ? AND target_id = ?`, projectID, string(target), from)
		if err != nil {
			return err
		}
		for _, l := range locks {
			var value json.RawMessage
			if l.value.Valid {
				value = json.RawMessage(l.value.String)
			}
			if err := upsertLock(ctx, tx, projectID, string(target), to, l.parameter, domain.LockMode(l.mode), value); err != nil {
				return err
			}
		}
		return nil
	}
	for _, c := range w.Characters {
		newID := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO character (id, project_id, branch_id, origin_id, name, face_reference_asset_id, body_reference_asset_id, hairstyle, wardrobe, voice, personality, emotion, position, current_action, voice_reference)
			 VALUES (?, ?, ?, (SELECT origin_id FROM character WHERE id = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT voice_reference FROM character WHERE id = ?))`,
			newID, projectID, branchID, c.ID, c.Name, nullIfEmpty(c.FaceReferenceAssetID), nullIfEmpty(c.BodyReferenceAssetID),
			nullIfEmpty(c.Hairstyle), nullIfEmpty(c.Wardrobe), nullIfEmpty(c.Voice), nullIfEmpty(c.Personality),
			nullIfEmpty(c.Emotion), nullIfEmpty(c.Position), nullIfEmpty(c.CurrentAction), c.ID); err != nil {
			return err
		}
		if err := copyLocks(domain.TargetCharacter, c.ID, newID); err != nil {
			return err
		}
	}
	for _, p := range w.Products {
		newID := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO product (id, project_id, branch_id, origin_id, name, geometry_reference_asset_id, label_reference_asset_id, material, orientation, condition)
			 VALUES (?, ?, ?, (SELECT origin_id FROM product WHERE id = ?), ?, ?, ?, ?, ?, ?)`,
			newID, projectID, branchID, p.ID, p.Name, nullIfEmpty(p.GeometryReferenceAssetID), nullIfEmpty(p.LabelReferenceAssetID),
			nullIfEmpty(p.Material), nullIfEmpty(p.Orientation), nullIfEmpty(p.Condition)); err != nil {
			return err
		}
		if err := copyLocks(domain.TargetProduct, p.ID, newID); err != nil {
			return err
		}
	}
	for _, l := range w.Locations {
		newID := domain.NewID()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO location (id, project_id, branch_id, origin_id, name, layout_reference_asset_id, time, lighting_state, weather)
			 VALUES (?, ?, ?, (SELECT origin_id FROM location WHERE id = ?), ?, ?, ?, ?, ?)`,
			newID, projectID, branchID, l.ID, l.Name, nullIfEmpty(l.LayoutReferenceAssetID),
			nullIfEmpty(l.Time), nullIfEmpty(l.LightingState), nullIfEmpty(l.Weather)); err != nil {
			return err
		}
		if err := copyLocks(domain.TargetLocation, l.ID, newID); err != nil {
			return err
		}
	}
	return nil
}

// appendBranchVersion appends the next branch_version of the Branch. Versions are never updated or deleted.
func (r *BranchRepository) appendBranchVersion(ctx context.Context, tx *sql.Tx, branchID, name, worldStateVersionID string) error {
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM branch_version WHERE branch_id = ?`, branchID).Scan(&next); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO branch_version (id, branch_id, version_number, name, world_state_version_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		domain.NewID(), branchID, next, name, nullIfEmpty(worldStateVersionID), FormatTime(r.now()))
	return err
}

// branchSelect reads a Branch with where it hangs in the tree: the Branch its Branch Point lies in
// (none for the trunk), the Scene / Shot numbers of the point and the numbers of its versions.
const branchSelect = `
SELECT b.id, b.project_id, COALESCE(ps.branch_id, shs.branch_id, ''), b.branch_point_type,
       COALESCE(b.branch_point_scene_id, ''), COALESCE(b.branch_point_shot_id, ''),
       COALESCE(ps.order_index, shs.order_index, -1) + 1, COALESCE(psh.order_index, -1) + 1,
       COALESCE(b.branch_point_offset_seconds, 0), b.name,
       COALESCE(b.world_state_version_id, ''), COALESCE(v.version_number, 0),
       (SELECT COALESCE(MAX(bv.version_number), 0) FROM branch_version bv WHERE bv.branch_id = b.id), b.created_at
FROM branch b
LEFT JOIN scene ps ON ps.id = b.branch_point_scene_id
LEFT JOIN shot psh ON psh.id = b.branch_point_shot_id
LEFT JOIN scene shs ON shs.id = psh.scene_id
LEFT JOIN world_state_version v ON v.id = b.world_state_version_id`

func scanBranch(row rowScanner) (domain.Branch, error) {
	var (
		b       domain.Branch
		created string
	)
	if err := row.Scan(&b.ID, &b.ProjectID, &b.ParentID, &b.PointType, &b.PointSceneID, &b.PointShotID,
		&b.PointSceneNumber, &b.PointShotNumber, &b.PointOffsetSeconds, &b.Name,
		&b.WorldStateVersionID, &b.WorldStateVersion, &b.Version, &created); err != nil {
		return domain.Branch{}, err
	}
	var err error
	b.CreatedAt, err = ParseTime(created)
	return b, err
}

// Get returns the Branch or ErrNotFound.
func (r *BranchRepository) Get(ctx context.Context, id string) (domain.Branch, error) {
	b, err := scanBranch(r.db.Reader().QueryRowContext(ctx, branchSelect+` WHERE b.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Branch{}, ErrNotFound
	}
	if err != nil {
		return domain.Branch{}, fmt.Errorf("get branch: %w", err)
	}
	return b, nil
}

// List returns the Branches of the Project, oldest first.
func (r *BranchRepository) List(ctx context.Context, projectID string) ([]domain.Branch, error) {
	out, err := queryAll(ctx, r.db.Reader(), scanBranch, branchSelect+` WHERE b.project_id = ? ORDER BY b.created_at, b.rowid`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	return out, nil
}

// BranchChange is what PUT /branches/:id may change. A nil field is left as it is.
type BranchChange struct {
	Name *string
	// WorldStateVersion is the number of a version of the Branch's own World State series.
	WorldStateVersion *int
}

// Update renames the Branch and/or points it at another World State version of its own series, and
// appends a branch_version for the change. A change that alters nothing appends nothing. It returns
// ErrNotFound when the Branch does not exist and ErrVersionNotFound when it has no such World State
// version.
func (r *BranchRepository) Update(ctx context.Context, id string, c BranchChange) (domain.Branch, error) {
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var projectID, name, versionID string
		err := tx.QueryRowContext(ctx, `SELECT project_id, name, COALESCE(world_state_version_id, '') FROM branch WHERE id = ?`, id).Scan(&projectID, &name, &versionID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		newName, newVersionID := name, versionID
		if c.Name != nil {
			newName = *c.Name
		}
		if c.WorldStateVersion != nil {
			err := tx.QueryRowContext(ctx,
				`SELECT id FROM world_state_version WHERE project_id = ? AND branch_id = ? AND version_number = ?`,
				projectID, id, *c.WorldStateVersion).Scan(&newVersionID)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrVersionNotFound
			}
			if err != nil {
				return err
			}
		}
		if newName == name && newVersionID == versionID {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE branch SET name = ?, world_state_version_id = ? WHERE id = ?`, newName, nullIfEmpty(newVersionID), id); err != nil {
			return err
		}
		return r.appendBranchVersion(ctx, tx, id, newName, newVersionID)
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrVersionNotFound) {
		return domain.Branch{}, err
	}
	if err != nil {
		return domain.Branch{}, fmt.Errorf("update branch: %w", err)
	}
	return r.Get(ctx, id)
}
