package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// NewReshoot is one additional Shot of a round of the Editor → Reshoot Loop.
type NewReshoot struct {
	ShotID          string
	AfterShotID     string
	Kind            domain.ClipKind
	Objective       string
	DurationSeconds float64
}

// CreateReshootRound records the additional Shots asked for by the Editor Judge of the Edit Timeline
// version as the next round of the Project's trunk or Branch, all status "requested". A version that
// does not exist is ErrNotFound.
func (r *EditorRepository) CreateReshootRound(ctx context.Context, timelineID string, reqs []NewReshoot) ([]domain.ReshootRequest, error) {
	var out []domain.ReshootRequest
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		out = out[:0]
		var projectID, branchID string
		err := tx.QueryRowContext(ctx, `SELECT project_id, COALESCE(branch_id, '') FROM edit_timeline_version WHERE id = ?`, timelineID).Scan(&projectID, &branchID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var round int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(round), 0) + 1 FROM reshoot_request WHERE project_id = ? AND branch_id IS ?`, projectID, nullIfEmpty(branchID)).Scan(&round); err != nil {
			return err
		}
		for _, n := range reqs {
			req := domain.ReshootRequest{
				ID: domain.NewID(), ProjectID: projectID, BranchID: branchID, EditTimelineVersionID: timelineID, Round: round,
				ShotID: n.ShotID, AfterShotID: n.AfterShotID, Kind: n.Kind, Objective: n.Objective, DurationSeconds: n.DurationSeconds,
				Status: domain.ReshootRequested, CreatedAt: r.now().UTC(),
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO reshoot_request (id, project_id, branch_id, edit_timeline_version_id, round, shot_id, after_shot_id, kind, objective, duration_seconds, status, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				req.ID, projectID, nullIfEmpty(branchID), timelineID, round, n.ShotID, nullIfEmpty(n.AfterShotID), n.Kind, n.Objective, n.DurationSeconds,
				req.Status, formatTime(req.CreatedAt)); err != nil {
				return err
			}
			out = append(out, req)
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("create reshoot round: %w", err)
	}
	return out, nil
}

// ReshootRounds is how many rounds of the Editor → Reshoot Loop the trunk of the Project (branchID
// "") or its Branch has had: what the Retry Policy counts.
func (r *EditorRepository) ReshootRounds(ctx context.Context, projectID, branchID string) (int, error) {
	var n int
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT round) FROM reshoot_request WHERE project_id = ? AND branch_id IS ?`, projectID, nullIfEmpty(branchID)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count reshoot rounds: %w", err)
	}
	return n, nil
}

const reshootSelect = `SELECT id, project_id, COALESCE(branch_id, ''), edit_timeline_version_id, round, shot_id, COALESCE(after_shot_id, ''),
	kind, objective, duration_seconds, status, COALESCE(applied_edit_timeline_version_id, ''), created_at FROM reshoot_request`

func (r *EditorRepository) queryReshoots(ctx context.Context, where string, args ...any) ([]domain.ReshootRequest, error) {
	rows, err := r.db.Reader().QueryContext(ctx, reshootSelect+where+` ORDER BY round, rowid`, args...)
	if err != nil {
		return nil, fmt.Errorf("list reshoot requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ReshootRequest
	for rows.Next() {
		var (
			q       domain.ReshootRequest
			created string
		)
		if err := rows.Scan(&q.ID, &q.ProjectID, &q.BranchID, &q.EditTimelineVersionID, &q.Round, &q.ShotID, &q.AfterShotID,
			&q.Kind, &q.Objective, &q.DurationSeconds, &q.Status, &q.AppliedEditTimelineVersionID, &created); err != nil {
			return nil, fmt.Errorf("scan reshoot request: %w", err)
		}
		if q.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// ListReshoots returns every additional Shot request of the Project (trunk and every Branch),
// oldest first. A Branch inherits the additional Shots of the series it split from, so the Shots of
// one series are the ones of this list that its story contains.
func (r *EditorRepository) ListReshoots(ctx context.Context, projectID string) ([]domain.ReshootRequest, error) {
	return r.queryReshoots(ctx, ` WHERE project_id = ?`, projectID)
}

// PendingReshoots returns the requests of the Project (every Branch) that have not been put into
// the Edit Timeline yet, oldest first.
func (r *EditorRepository) PendingReshoots(ctx context.Context, projectID string) ([]domain.ReshootRequest, error) {
	return r.queryReshoots(ctx, ` WHERE project_id = ? AND status = ?`, projectID, domain.ReshootRequested)
}

// ProjectsWithPendingReshoots returns the IDs of the Projects that wait for an additional Shot.
func (r *EditorRepository) ProjectsWithPendingReshoots(ctx context.Context) ([]string, error) {
	rows, err := r.db.Reader().QueryContext(ctx, `SELECT DISTINCT project_id FROM reshoot_request WHERE status = ?`, domain.ReshootRequested)
	if err != nil {
		return nil, fmt.Errorf("list projects with pending reshoots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan project id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SaveReshootApplied saves the timeline that has the additional Shots as the next version of the
// trunk / Branch and marks the requests applied, in one transaction: a request is applied exactly
// once. baseID must still be the latest version of the series (ErrStale otherwise); a request that
// was already applied is ErrConflict.
func (r *EditorRepository) SaveReshootApplied(ctx context.Context, projectID, branchID, baseID string, tl domain.EditTimeline, requestIDs []string) (domain.EditTimelineVersion, error) {
	ver, err := r.saveEditTimeline(ctx, projectID, branchID, baseID, tl, func(ctx context.Context, tx *sql.Tx, versionID string) error {
		for _, id := range requestIDs {
			res, err := tx.ExecContext(ctx,
				`UPDATE reshoot_request SET status = ?, applied_edit_timeline_version_id = ? WHERE id = ? AND status = ?`,
				domain.ReshootApplied, versionID, id, domain.ReshootRequested)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return ErrConflict
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrStale) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			return domain.EditTimelineVersion{}, err
		}
		return domain.EditTimelineVersion{}, fmt.Errorf("apply reshoot requests: %w", err)
	}
	return ver, nil
}
