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

// ErrStale means a timeline was saved against a version that is no longer the latest one.
var ErrStale = errors.New("repository: the timeline has a newer version")

// EditorRepository persists the Edit Timeline, the Editor Judge results, the Audio Timeline, the
// Final QC results and the Exports of a Project (spec/architecture/er.md §2.3). Timelines are
// versioned: every save appends the next version. All writes go through DB.Write.
type EditorRepository struct {
	db  *DB
	now func() time.Time
}

// NewEditorRepository returns an EditorRepository backed by db.
func NewEditorRepository(db *DB) *EditorRepository {
	return &EditorRepository{db: db, now: time.Now}
}

// SaveEditTimeline appends the next Edit Timeline version of the trunk of the Project (branchID
// "") or of its Branch: every Branch numbers its own versions and never touches another's. When
// baseID is not empty the save only succeeds if that version is still the latest of the series
// (ErrStale otherwise), so that an editor that was open on an old version cannot overwrite a newer
// one. A Branch of another Project is ErrNotFound.
func (r *EditorRepository) SaveEditTimeline(ctx context.Context, projectID, branchID, baseID string, tl domain.EditTimeline) (domain.EditTimelineVersion, error) {
	v, err := r.saveEditTimeline(ctx, projectID, branchID, baseID, tl, nil)
	if err != nil {
		if errors.Is(err, ErrStale) || errors.Is(err, ErrNotFound) {
			return domain.EditTimelineVersion{}, err
		}
		return domain.EditTimelineVersion{}, fmt.Errorf("save edit timeline: %w", err)
	}
	return v, nil
}

// saveEditTimeline is SaveEditTimeline with after, run in the same transaction once the version is
// written. Errors are not wrapped.
func (r *EditorRepository) saveEditTimeline(ctx context.Context, projectID, branchID, baseID string, tl domain.EditTimeline, after func(ctx context.Context, tx *sql.Tx, versionID string) error) (domain.EditTimelineVersion, error) {
	raw, err := json.Marshal(tl)
	if err != nil {
		return domain.EditTimelineVersion{}, fmt.Errorf("marshal edit timeline: %w", err)
	}
	v := domain.EditTimelineVersion{ID: domain.NewID(), ProjectID: projectID, BranchID: branchID, Timeline: tl, CreatedAt: r.now().UTC()}
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		if branchID != "" {
			var one int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM branch WHERE id = ? AND project_id = ?`, branchID, projectID).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		var latestID string
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM edit_timeline_version WHERE project_id = ? AND branch_id IS ? ORDER BY version_number DESC LIMIT 1`,
			projectID, nullIfEmpty(branchID)).Scan(&latestID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if baseID != "" && latestID != baseID {
			return ErrStale
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(version_number), 0) + 1 FROM edit_timeline_version WHERE project_id = ? AND branch_id IS ?`,
			projectID, nullIfEmpty(branchID)).Scan(&v.VersionNumber); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO edit_timeline_version (id, project_id, branch_id, version_number, timeline, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			v.ID, projectID, nullIfEmpty(branchID), v.VersionNumber, string(raw), formatTime(v.CreatedAt)); err != nil {
			return err
		}
		if after != nil {
			return after(ctx, tx, v.ID)
		}
		return nil
	})
	if err != nil {
		return domain.EditTimelineVersion{}, err
	}
	return v, nil
}

func scanEditTimeline(row rowScanner) (domain.EditTimelineVersion, error) {
	var (
		v       domain.EditTimelineVersion
		raw     string
		created string
	)
	if err := row.Scan(&v.ID, &v.ProjectID, &v.BranchID, &v.VersionNumber, &raw, &created); err != nil {
		return domain.EditTimelineVersion{}, err
	}
	if err := json.Unmarshal([]byte(raw), &v.Timeline); err != nil {
		return domain.EditTimelineVersion{}, fmt.Errorf("decode edit timeline: %w", err)
	}
	var err error
	v.CreatedAt, err = parseTime(created)
	return v, err
}

const editTimelineSelect = `SELECT id, project_id, COALESCE(branch_id, ''), version_number, timeline, created_at FROM edit_timeline_version`

// GetEditTimeline returns an Edit Timeline version or ErrNotFound.
func (r *EditorRepository) GetEditTimeline(ctx context.Context, id string) (domain.EditTimelineVersion, error) {
	v, err := scanEditTimeline(r.db.Reader().QueryRowContext(ctx, editTimelineSelect+` WHERE id = ?`, id))
	return v, notFoundOr(err, "get edit timeline")
}

// LatestEditTimeline returns the newest Edit Timeline version of the trunk of the Project (branchID
// "") or of its Branch, or ErrNotFound.
func (r *EditorRepository) LatestEditTimeline(ctx context.Context, projectID, branchID string) (domain.EditTimelineVersion, error) {
	v, err := scanEditTimeline(r.db.Reader().QueryRowContext(ctx,
		editTimelineSelect+` WHERE project_id = ? AND branch_id IS ? ORDER BY version_number DESC LIMIT 1`, projectID, nullIfEmpty(branchID)))
	return v, notFoundOr(err, "get latest edit timeline")
}

// notFoundOr maps sql.ErrNoRows to ErrNotFound and wraps any other error.
func notFoundOr(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// SaveEditorJudge stores the result of an Editor Judge run.
func (r *EditorRepository) SaveEditorJudge(ctx context.Context, editTimelineID string, verdict domain.EditorVerdict, findings []domain.EditorFinding) (domain.EditorJudgeResult, error) {
	if findings == nil {
		findings = []domain.EditorFinding{}
	}
	raw, err := json.Marshal(findings)
	if err != nil {
		return domain.EditorJudgeResult{}, fmt.Errorf("marshal findings: %w", err)
	}
	res := domain.EditorJudgeResult{ID: domain.NewID(), EditTimelineVersionID: editTimelineID, Verdict: verdict, Findings: findings, CreatedAt: r.now().UTC()}
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO editor_judge_result (id, edit_timeline_version_id, result, findings, created_at) VALUES (?, ?, ?, ?, ?)`,
			res.ID, editTimelineID, verdict, string(raw), formatTime(res.CreatedAt))
		return err
	})
	if err != nil {
		return domain.EditorJudgeResult{}, fmt.Errorf("save editor judge result: %w", err)
	}
	return res, nil
}

// LatestEditorJudge returns the newest Editor Judge result of the Edit Timeline version or ErrNotFound.
func (r *EditorRepository) LatestEditorJudge(ctx context.Context, editTimelineID string) (domain.EditorJudgeResult, error) {
	var (
		res     domain.EditorJudgeResult
		raw     string
		created string
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT id, edit_timeline_version_id, result, findings, created_at FROM editor_judge_result
		 WHERE edit_timeline_version_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, editTimelineID).
		Scan(&res.ID, &res.EditTimelineVersionID, &res.Verdict, &raw, &created)
	if err != nil {
		return domain.EditorJudgeResult{}, notFoundOr(err, "get editor judge result")
	}
	if err := json.Unmarshal([]byte(raw), &res.Findings); err != nil {
		return domain.EditorJudgeResult{}, fmt.Errorf("decode findings: %w", err)
	}
	res.CreatedAt, err = parseTime(created)
	return res, err
}

// SaveAudioTimeline appends the next Audio Timeline version of the trunk of the Project (branchID
// "") or of its Branch: every Branch numbers its own versions and never touches another's. A
// Branch of another Project is ErrNotFound.
func (r *EditorRepository) SaveAudioTimeline(ctx context.Context, projectID, branchID string, tl domain.AudioTimeline) (domain.AudioTimelineVersion, error) {
	raw, err := json.Marshal(tl)
	if err != nil {
		return domain.AudioTimelineVersion{}, fmt.Errorf("marshal audio timeline: %w", err)
	}
	v := domain.AudioTimelineVersion{ID: domain.NewID(), ProjectID: projectID, BranchID: branchID, Timeline: tl, CreatedAt: r.now().UTC()}
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := projectExists(ctx, tx, projectID); err != nil {
			return err
		}
		if branchID != "" {
			var one int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM branch WHERE id = ? AND project_id = ?`, branchID, projectID).Scan(&one)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(version_number), 0) + 1 FROM audio_timeline_version WHERE project_id = ? AND branch_id IS ?`,
			projectID, nullIfEmpty(branchID)).Scan(&v.VersionNumber); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO audio_timeline_version (id, project_id, branch_id, version_number, timeline, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			v.ID, projectID, nullIfEmpty(branchID), v.VersionNumber, string(raw), formatTime(v.CreatedAt))
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.AudioTimelineVersion{}, err
		}
		return domain.AudioTimelineVersion{}, fmt.Errorf("save audio timeline: %w", err)
	}
	return v, nil
}

func scanAudioTimeline(row rowScanner) (domain.AudioTimelineVersion, error) {
	var (
		v       domain.AudioTimelineVersion
		raw     string
		created string
	)
	if err := row.Scan(&v.ID, &v.ProjectID, &v.BranchID, &v.VersionNumber, &raw, &created); err != nil {
		return domain.AudioTimelineVersion{}, err
	}
	if err := json.Unmarshal([]byte(raw), &v.Timeline); err != nil {
		return domain.AudioTimelineVersion{}, fmt.Errorf("decode audio timeline: %w", err)
	}
	var err error
	v.CreatedAt, err = parseTime(created)
	return v, err
}

const audioTimelineSelect = `SELECT id, project_id, COALESCE(branch_id, ''), version_number, timeline, created_at FROM audio_timeline_version`

// LatestAudioTimeline returns the newest Audio Timeline version of the trunk of the Project
// (branchID "") or of its Branch, or ErrNotFound.
func (r *EditorRepository) LatestAudioTimeline(ctx context.Context, projectID, branchID string) (domain.AudioTimelineVersion, error) {
	v, err := scanAudioTimeline(r.db.Reader().QueryRowContext(ctx,
		audioTimelineSelect+` WHERE project_id = ? AND branch_id IS ? ORDER BY version_number DESC LIMIT 1`, projectID, nullIfEmpty(branchID)))
	return v, notFoundOr(err, "get latest audio timeline")
}

// ListAudioTimelines returns every Audio Timeline version of the trunk of the Project (branchID
// "") or of its Branch, newest first.
func (r *EditorRepository) ListAudioTimelines(ctx context.Context, projectID, branchID string) ([]domain.AudioTimelineVersion, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		audioTimelineSelect+` WHERE project_id = ? AND branch_id IS ? ORDER BY version_number DESC`, projectID, nullIfEmpty(branchID))
	if err != nil {
		return nil, fmt.Errorf("list audio timelines: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.AudioTimelineVersion
	for rows.Next() {
		v, err := scanAudioTimeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SaveFinalQC stores the result of a Final QC run.
func (r *EditorRepository) SaveFinalQC(ctx context.Context, projectID, audioTimelineID string, verdict domain.FinalQCVerdict, findings []domain.QCFinding) (domain.FinalQCResult, error) {
	if findings == nil {
		findings = []domain.QCFinding{}
	}
	raw, err := json.Marshal(findings)
	if err != nil {
		return domain.FinalQCResult{}, fmt.Errorf("marshal findings: %w", err)
	}
	res := domain.FinalQCResult{ID: domain.NewID(), ProjectID: projectID, AudioTimelineVersionID: audioTimelineID, Verdict: verdict, Findings: findings, CreatedAt: r.now().UTC()}
	err = r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO final_qc_result (id, project_id, audio_timeline_version_id, result, findings, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			res.ID, projectID, audioTimelineID, verdict, string(raw), formatTime(res.CreatedAt))
		return err
	})
	if err != nil {
		return domain.FinalQCResult{}, fmt.Errorf("save final qc result: %w", err)
	}
	return res, nil
}

// LatestFinalQC returns the newest Final QC result of the Project or ErrNotFound.
func (r *EditorRepository) LatestFinalQC(ctx context.Context, projectID string) (domain.FinalQCResult, error) {
	var (
		res     domain.FinalQCResult
		raw     string
		created string
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT id, project_id, audio_timeline_version_id, result, findings, created_at FROM final_qc_result
		 WHERE project_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, projectID).
		Scan(&res.ID, &res.ProjectID, &res.AudioTimelineVersionID, &res.Verdict, &raw, &created)
	if err != nil {
		return domain.FinalQCResult{}, notFoundOr(err, "get final qc result")
	}
	if err := json.Unmarshal([]byte(raw), &res.Findings); err != nil {
		return domain.FinalQCResult{}, fmt.Errorf("decode findings: %w", err)
	}
	res.CreatedAt, err = parseTime(created)
	return res, err
}

// CreateExport records a finished export.
func (r *EditorRepository) CreateExport(ctx context.Context, e domain.Export) (domain.Export, error) {
	e.ExportedAt = r.now().UTC()
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec, exported_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.ID, e.ProjectID, e.FilePath, e.Resolution, e.AspectRatio, e.Codec, formatTime(e.ExportedAt))
		return err
	})
	if err != nil {
		return domain.Export{}, fmt.Errorf("create export: %w", err)
	}
	return e, nil
}

func scanExport(row rowScanner) (domain.Export, error) {
	var (
		e        domain.Export
		exported string
	)
	if err := row.Scan(&e.ID, &e.ProjectID, &e.FilePath, &e.Resolution, &e.AspectRatio, &e.Codec, &exported); err != nil {
		return domain.Export{}, err
	}
	var err error
	e.ExportedAt, err = parseTime(exported)
	return e, err
}

const exportSelect = `SELECT id, project_id, file_path, resolution, aspect_ratio, codec, exported_at FROM export`

// GetExport returns an Export of the Project or ErrNotFound.
func (r *EditorRepository) GetExport(ctx context.Context, projectID, id string) (domain.Export, error) {
	e, err := scanExport(r.db.Reader().QueryRowContext(ctx, exportSelect+` WHERE project_id = ? AND id = ?`, projectID, id))
	return e, notFoundOr(err, "get export")
}

// ListExports returns the Exports of the Project, newest first.
func (r *EditorRepository) ListExports(ctx context.Context, projectID string) ([]domain.Export, error) {
	rows, err := r.db.Reader().QueryContext(ctx, exportSelect+` WHERE project_id = ? ORDER BY exported_at DESC, rowid DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list exports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Export
	for rows.Next() {
		e, err := scanExport(rows)
		if err != nil {
			return nil, fmt.Errorf("scan export: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
