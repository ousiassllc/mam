package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Scenes returns the Scenes of the Branch's own derived tree in order.
func (r *BranchRepository) Scenes(ctx context.Context, branchID string) ([]domain.Scene, error) {
	scenes, err := queryAll(ctx, r.db.Reader(), scanScene, sceneSelect+` WHERE s.branch_id = ? ORDER BY s.order_index`, branchID)
	if err != nil {
		return nil, fmt.Errorf("list branch scenes: %w", err)
	}
	return scenes, nil
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
	own, err := queryAll(ctx, point, scanShotRef, shotRefSelect+` WHERE s.project_id = ? AND s.branch_id = ? ORDER BY s.order_index, sh.order_index`, projectID, branchID)
	if err != nil {
		return nil, fmt.Errorf("list branch shots: %w", err)
	}
	return append(append([]ShotRef{}, inherited...), own...), nil
}

// Versions returns the branch_versions of the Branch, newest first.
func (r *BranchRepository) Versions(ctx context.Context, branchID string) ([]domain.BranchVersion, error) {
	out, err := queryAll(ctx, r.db.Reader(), func(row rowScanner) (domain.BranchVersion, error) {
		var (
			v       domain.BranchVersion
			created string
		)
		if err := row.Scan(&v.ID, &v.BranchID, &v.Number, &v.Name, &v.WorldStateVersionID, &v.WorldStateVersion, &created); err != nil {
			return v, fmt.Errorf("scan branch version: %w", err)
		}
		var err error
		v.CreatedAt, err = ParseTime(created)
		return v, err
	}, `
		SELECT bv.id, bv.branch_id, bv.version_number, bv.name, COALESCE(bv.world_state_version_id, ''), COALESCE(v.version_number, 0), bv.created_at
		FROM branch_version bv LEFT JOIN world_state_version v ON v.id = bv.world_state_version_id
		WHERE bv.branch_id = ? ORDER BY bv.version_number DESC`, branchID)
	if err != nil {
		return nil, fmt.Errorf("list branch versions: %w", err)
	}
	return out, nil
}

// ListAllShots returns every Shot of the Project, trunk first and then each Branch's own tree, in
// story order within each.
func (r *GenerationRepository) ListAllShots(ctx context.Context, projectID string) ([]ShotRef, error) {
	out, err := queryAll(ctx, r.db.Reader(), scanShotRef,
		shotRefSelect+` WHERE s.project_id = ? ORDER BY COALESCE(s.branch_id, ''), s.order_index, sh.order_index`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list all shots: %w", err)
	}
	return out, nil
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
