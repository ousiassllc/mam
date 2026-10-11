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

// ErrVersionNotFound means the World State version a Branch was told to refer to does not exist
// in the Branch's own version series.
var ErrVersionNotFound = errors.New("repository: world state version not found")

// BranchRepository persists Branches (F-17, spec/architecture/er.md §4 "Branch"): the Branch Point
// with the Snapshot of the World State taken there, and the Branch's own World State, which starts
// as a copy of the Snapshot and is never shared with the trunk or another Branch. All writes go
// through DB.Write.
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

// snapshotOf returns the World State to snapshot at a Branch Point of the series (trunk or Branch)
// the Shot lies in: the state the Shot left behind for shot and frame, the state the last Shot
// before the Scene left behind for scene. When no Shot has completed there, it is the current
// state of the series.
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
		rows, err := tx.QueryContext(ctx,
			`SELECT parameter_name, mode, value FROM lock WHERE project_id = ? AND target_type = ? AND target_id = ?`, projectID, string(target), from)
		if err != nil {
			return err
		}
		type lockRow struct {
			parameter, mode string
			value           sql.NullString
		}
		var locks []lockRow
		for rows.Next() {
			var l lockRow
			if err := rows.Scan(&l.parameter, &l.mode, &l.value); err != nil {
				_ = rows.Close()
				return err
			}
			locks = append(locks, l)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
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
			`INSERT INTO character (id, project_id, branch_id, origin_id, name, face_reference_asset_id, body_reference_asset_id, hairstyle, wardrobe, voice, personality, emotion, position, current_action)
			 VALUES (?, ?, ?, (SELECT origin_id FROM character WHERE id = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newID, projectID, branchID, c.ID, c.Name, nullIfEmpty(c.FaceReferenceAssetID), nullIfEmpty(c.BodyReferenceAssetID),
			nullIfEmpty(c.Hairstyle), nullIfEmpty(c.Wardrobe), nullIfEmpty(c.Voice), nullIfEmpty(c.Personality),
			nullIfEmpty(c.Emotion), nullIfEmpty(c.Position), nullIfEmpty(c.CurrentAction)); err != nil {
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
	rows, err := r.db.Reader().QueryContext(ctx, branchSelect+` WHERE b.project_id = ? ORDER BY b.created_at, b.rowid`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Branch
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan branch: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
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

// Scenes returns the Scenes of the Branch's own derived tree in order.
func (r *BranchRepository) Scenes(ctx context.Context, branchID string) ([]domain.Scene, error) {
	rows, err := r.db.Reader().QueryContext(ctx, sceneSelect+` WHERE s.branch_id = ? ORDER BY s.order_index`, branchID)
	if err != nil {
		return nil, fmt.Errorf("list branch scenes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scenes []domain.Scene
	for rows.Next() {
		s, err := scanScene(rows)
		if err != nil {
			return nil, fmt.Errorf("scan scene: %w", err)
		}
		scenes = append(scenes, s)
	}
	return scenes, rows.Err()
}

// CutAtBranchPoint returns the Shots of a story (in order) that a Branch made at the point inherits:
// up to and including the Branch Point's Shot, or for a Branch at a Scene the Shots before that
// Scene. A point that is not in the story keeps all of it.
func CutAtBranchPoint(story []ShotRef, typ domain.BranchPointType, sceneID, shotID string) []ShotRef {
	for i, s := range story {
		if typ == domain.BranchAtScene && s.Shot.SceneID == sceneID {
			return story[:i]
		}
		if typ != domain.BranchAtScene && s.Shot.ID == shotID {
			return story[:i+1]
		}
	}
	return story
}

// StoryShots returns the Shots of the story of the trunk (branchID "") or of the Branch in story
// order: for a Branch, the Shots of the series it hangs from up to its Branch Point, then the Shots
// of its own derived tree.
func (r *GenerationRepository) StoryShots(ctx context.Context, projectID, branchID string) ([]ShotRef, error) {
	if branchID == "" {
		return r.ListProjectShots(ctx, projectID)
	}
	var (
		typ             domain.BranchPointType
		sceneID, shotID string
		parentID        string
		point           = r.db.Reader()
	)
	err := point.QueryRowContext(ctx, `
		SELECT b.branch_point_type, COALESCE(b.branch_point_scene_id, ''), COALESCE(b.branch_point_shot_id, ''), COALESCE(ps.branch_id, shs.branch_id, '')
		FROM branch b
		LEFT JOIN scene ps ON ps.id = b.branch_point_scene_id
		LEFT JOIN shot psh ON psh.id = b.branch_point_shot_id
		LEFT JOIN scene shs ON shs.id = psh.scene_id
		WHERE b.id = ? AND b.project_id = ?`, branchID, projectID).Scan(&typ, &sceneID, &shotID, &parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get branch point: %w", err)
	}
	inherited, err := r.StoryShots(ctx, projectID, parentID)
	if err != nil {
		return nil, err
	}
	inherited = CutAtBranchPoint(inherited, typ, sceneID, shotID)
	rows, err := point.QueryContext(ctx, shotRefSelect+` WHERE s.project_id = ? AND s.branch_id = ? ORDER BY s.order_index, sh.order_index`, projectID, branchID)
	if err != nil {
		return nil, fmt.Errorf("list branch shots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := append([]ShotRef{}, inherited...)
	for rows.Next() {
		ref, err := scanShotRef(rows)
		if err != nil {
			return nil, fmt.Errorf("scan shot: %w", err)
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// Versions returns the branch_versions of the Branch, newest first.
func (r *BranchRepository) Versions(ctx context.Context, branchID string) ([]domain.BranchVersion, error) {
	rows, err := r.db.Reader().QueryContext(ctx, `
		SELECT bv.id, bv.branch_id, bv.version_number, bv.name, COALESCE(bv.world_state_version_id, ''), COALESCE(v.version_number, 0), bv.created_at
		FROM branch_version bv LEFT JOIN world_state_version v ON v.id = bv.world_state_version_id
		WHERE bv.branch_id = ? ORDER BY bv.version_number DESC`, branchID)
	if err != nil {
		return nil, fmt.Errorf("list branch versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.BranchVersion
	for rows.Next() {
		var (
			v       domain.BranchVersion
			created string
		)
		if err := rows.Scan(&v.ID, &v.BranchID, &v.Number, &v.Name, &v.WorldStateVersionID, &v.WorldStateVersion, &created); err != nil {
			return nil, fmt.Errorf("scan branch version: %w", err)
		}
		if v.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListAllShots returns every Shot of the Project, trunk first and then each Branch's own tree, in
// story order within each.
func (r *GenerationRepository) ListAllShots(ctx context.Context, projectID string) ([]ShotRef, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		shotRefSelect+` WHERE s.project_id = ? ORDER BY COALESCE(s.branch_id, ''), s.order_index, sh.order_index`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list all shots: %w", err)
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

// LatestWorldVersion returns the number of the newest world_state_version of the Branch's own
// series (0 when it has none).
func (r *BranchRepository) LatestWorldVersion(ctx context.Context, branchID string) (int, error) {
	var n int
	err := r.db.Reader().QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) FROM world_state_version WHERE branch_id = ?`, branchID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("latest branch world version: %w", err)
	}
	return n, nil
}
