package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// StorageRepository reads and frees the disk usage of a Project: the Take videos, which are by far
// the biggest files (spec/requirements/non-functional.md §4). All writes go through DB.Write.
type StorageRepository struct{ db *DB }

// NewStorageRepository returns a StorageRepository backed by db.
func NewStorageRepository(db *DB) *StorageRepository { return &StorageRepository{db: db} }

// takeInUse is the SQL condition (on take t, scene s of its Shot) of a Take that the editing still
// refers to: a clip of the latest Edit Timeline of the trunk or of a Branch, or a clip the latest Audio
// Timeline of the trunk or of a Branch redid by Lip Sync. Choosing another Take of the Shot does not
// move such a clip, and export needs the file, so the file must stay.
const takeInUse = `t.id IN (
	SELECT json_extract(c.value, '$.take_id')
	FROM edit_timeline_version e, json_each(e.timeline, '$.clips') c
	WHERE e.project_id = s.project_id AND e.version_number = (
		SELECT MAX(e2.version_number) FROM edit_timeline_version e2
		WHERE e2.project_id = e.project_id AND COALESCE(e2.branch_id, '') = COALESCE(e.branch_id, ''))
	UNION
	SELECT json_extract(l.value, '$.take_id')
	FROM audio_timeline_version a, json_each(a.timeline, '$.lip_sync') l
	WHERE a.project_id = s.project_id AND a.version_number = (
		SELECT MAX(a2.version_number) FROM audio_timeline_version a2
		WHERE a2.project_id = a.project_id AND COALESCE(a2.branch_id, '') = COALESCE(a.branch_id, '')))`

// takeUnneeded is the SQL condition (on take t, prompt_version pv, scene s) of a Take that can be
// archived without losing a choice or a file the editing uses: it is Rejected, or another Take of its
// Shot is Selected, and no Timeline refers to it.
const takeUnneeded = `((t.status = 'rejected' OR (t.status <> 'selected' AND EXISTS (
	SELECT 1 FROM take t2 JOIN prompt_version pv2 ON pv2.id = t2.prompt_version_id
	WHERE pv2.shot_id = pv.shot_id AND t2.status = 'selected'))) AND NOT ` + takeInUse + `)`

// takeArchivable is the SQL condition (on take t, Shot sh, scene s) under which a Take's video may be
// given up: it is not the Selected Take of its Shot and no Timeline refers to it. Usage shows the
// archive button by it and ArchiveTake enforces it, so the two never disagree.
const takeArchivable = `(t.status <> 'selected' AND COALESCE(sh.selected_take_id, '') <> t.id AND NOT ` + takeInUse + `)`

// Usage returns the size of the files of the Project and its stored Takes, the unneeded ones first.
func (r *StorageRepository) Usage(ctx context.Context, projectID string) (domain.StorageSummary, error) {
	var sum domain.StorageSummary
	var exists int
	err := r.db.Reader().QueryRowContext(ctx, `SELECT 1 FROM project WHERE id = ?`, projectID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.StorageSummary{}, ErrNotFound
	}
	if err != nil {
		return domain.StorageSummary{}, fmt.Errorf("get project: %w", err)
	}
	if err := r.db.Reader().QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size_bytes), 0) FROM asset WHERE project_id = ?`, projectID).Scan(&sum.AssetBytes); err != nil {
		return domain.StorageSummary{}, fmt.Errorf("sum assets: %w", err)
	}

	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT t.id, pv.shot_id, s.title, s.order_index, sh.order_index, t.status, t.model_name, COALESCE(a.size_bytes, 0), `+takeUnneeded+`, `+takeArchivable+`
		 FROM take t
		 JOIN prompt_version pv ON pv.id = t.prompt_version_id
		 JOIN shot sh ON sh.id = pv.shot_id
		 JOIN scene s ON s.id = sh.scene_id
		 LEFT JOIN asset a ON a.project_id = s.project_id AND a.file_path = t.file_path
		 WHERE s.project_id = ? AND COALESCE(t.file_path, '') <> ''
		 ORDER BY s.order_index, sh.order_index, t.created_at, t.id`, projectID)
	if err != nil {
		return domain.StorageSummary{}, fmt.Errorf("list stored takes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t domain.StoredTake
		if err := rows.Scan(&t.TakeID, &t.ShotID, &t.SceneTitle, &t.SceneIndex, &t.ShotIndex, &t.Status, &t.ModelName, &t.SizeBytes, &t.Unneeded, &t.Archivable); err != nil {
			return domain.StorageSummary{}, fmt.Errorf("scan stored take: %w", err)
		}
		sum.Takes = append(sum.Takes, t)
	}
	if err := rows.Err(); err != nil {
		return domain.StorageSummary{}, err
	}
	slices.SortStableFunc(sum.Takes, func(a, b domain.StoredTake) int {
		switch {
		case a.Unneeded && !b.Unneeded:
			return -1
		case !a.Unneeded && b.Unneeded:
			return 1
		}
		return 0
	})
	return sum, nil
}

// ArchiveTake archives one Take of the Project: its video file is given up, the Take stays as a
// Rejected row with its Judge and Repair history (F-27 traceability). It returns the removed assets
// so that the caller can delete their files. It returns ErrNotFound for a Take that is not stored in the
// Project and ErrConflict for the Selected Take of a Shot and for a Take a Timeline still refers to.
func (r *StorageRepository) ArchiveTake(ctx context.Context, projectID, takeID string) ([]domain.Asset, error) {
	var removed []domain.Asset
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var path string
		var archivable bool
		err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(t.file_path, ''), `+takeArchivable+`
			 FROM take t JOIN prompt_version pv ON pv.id = t.prompt_version_id
			 JOIN shot sh ON sh.id = pv.shot_id JOIN scene s ON s.id = sh.scene_id
			 WHERE t.id = ? AND s.project_id = ?`, takeID, projectID).Scan(&path, &archivable)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && path == "") {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !archivable {
			return ErrConflict
		}
		removed, err = archive(ctx, tx, projectID, []takeFile{{takeID, path}})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("archive take: %w", err)
	}
	return removed, nil
}

// ArchiveUnneeded archives every unneeded Take of the Project (Rejected, or beaten by a Selected
// Take of the same Shot) and returns the removed assets.
func (r *StorageRepository) ArchiveUnneeded(ctx context.Context, projectID string) ([]domain.Asset, error) {
	var removed []domain.Asset
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT t.id, t.file_path FROM take t
			 JOIN prompt_version pv ON pv.id = t.prompt_version_id
			 JOIN shot sh ON sh.id = pv.shot_id JOIN scene s ON s.id = sh.scene_id
			 WHERE s.project_id = ? AND COALESCE(t.file_path, '') <> '' AND `+takeUnneeded+`
			   AND COALESCE(sh.selected_take_id, '') <> t.id`, projectID)
		if err != nil {
			return err
		}
		var files []takeFile
		for rows.Next() {
			var f takeFile
			if err := rows.Scan(&f.takeID, &f.path); err != nil {
				_ = rows.Close()
				return err
			}
			files = append(files, f)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		removed, err = archive(ctx, tx, projectID, files)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("archive unneeded takes: %w", err)
	}
	return removed, nil
}

type takeFile struct{ takeID, path string }

// archive detaches the video files of the Takes: the Takes become Rejected without a file and the
// asset rows of the files are deleted.
func archive(ctx context.Context, tx *sql.Tx, projectID string, files []takeFile) ([]domain.Asset, error) {
	var removed []domain.Asset
	for _, f := range files {
		if _, err := tx.ExecContext(ctx, `UPDATE take SET status = 'rejected', file_path = NULL WHERE id = ?`, f.takeID); err != nil {
			return nil, err
		}
		rows, err := tx.QueryContext(ctx, `DELETE FROM asset WHERE project_id = ? AND file_path = ? RETURNING id, type, file_path, COALESCE(size_bytes, 0)`, projectID, f.path)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			a := domain.Asset{ProjectID: projectID}
			if err := rows.Scan(&a.ID, &a.Type, &a.FilePath, &a.SizeBytes); err != nil {
				_ = rows.Close()
				return nil, err
			}
			removed = append(removed, a)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return removed, nil
}
