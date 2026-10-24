package gentestrepo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ProjectImage is an image of a Project the generation test screen can take as an input: an image asset, with the entry of the
// Reference Library that holds it when there is one.
type ProjectImage struct {
	AssetID         string
	Type            string // asset.type: image or reference
	MimeType        string
	SizeBytes       int64
	ReferenceItemID string // "" when the asset is not in the Reference Library
	Kind            string // the library kind of the entry
	Name            string // the name of the entry
	CreatedAt       time.Time
}

// ProjectImages lists the image assets of a Project, newest first. It reads the Project's tables (read only).
func (r *Repository) ProjectImages(ctx context.Context, projectID string) ([]ProjectImage, error) {
	return queryAll(ctx, r, func(s scanner) (ProjectImage, error) {
		var (
			p       ProjectImage
			created string
		)
		if err := s.Scan(&p.AssetID, &p.Type, &p.MimeType, &p.SizeBytes, &p.ReferenceItemID, &p.Kind, &p.Name, &created); err != nil {
			return ProjectImage{}, err
		}
		var err error
		p.CreatedAt, err = parse(created)
		return p, err
	}, `SELECT a.id, a.type, COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(ri.id, ''), COALESCE(ri.kind, ''), COALESCE(ri.name, ''), a.created_at
		FROM asset a LEFT JOIN reference_item ri ON ri.asset_id = a.id
		WHERE a.project_id = ? AND a.type IN ('image', 'reference') AND COALESCE(a.mime_type, '') LIKE 'image/%'
		ORDER BY a.created_at DESC, a.id DESC`, projectID)
}

// AddProjectAsset writes an asset row of type image or video for an imported result. It is the one write this repository
// makes into a Project's tables, and only because the import asked for it. An unknown Project is repository.ErrNotFound.
func (r *Repository) AddProjectAsset(ctx context.Context, a domain.Asset) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM project WHERE id = ?`, a.ProjectID).Scan(&one); err != nil {
			return notFound(err)
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO asset (id, project_id, type, file_path, mime_type, size_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.ProjectID, a.Type, a.FilePath, a.MimeType, a.SizeBytes, repository.FormatTime(r.now()))
		if err != nil {
			return fmt.Errorf("insert asset: %w", err)
		}
		return nil
	})
}

// ListBatches returns the batches newest first with the number of their inputs and results.
func (r *Repository) ListBatches(ctx context.Context, limit int) ([]Batch, error) {
	if limit <= 0 {
		limit = 50
	}
	return queryAll(ctx, r, func(s scanner) (Batch, error) {
		var (
			b                      Batch
			params, estimate, made string
			finished               sql.NullString
		)
		if err := s.Scan(&b.ID, &b.Media, &b.JobID, &params, &b.EstimatedCost, &estimate, &made, &finished, &b.InputCount, &b.ResultCount, &b.SucceededCount); err != nil {
			return Batch{}, err
		}
		b.Params, b.Estimate = []byte(params), []byte(estimate)
		var err error
		if b.CreatedAt, err = parse(made); err != nil {
			return Batch{}, err
		}
		b.FinishedAt, err = parseOpt(finished)
		return b, err
	}, `SELECT b.id, b.media, b.job_id, b.params, b.estimated_cost, b.estimate, b.created_at, b.finished_at,
			(SELECT COUNT(*) FROM gentest_input i WHERE i.batch_id = b.id),
			(SELECT COUNT(*) FROM gentest_result x WHERE x.batch_id = b.id),
			(SELECT COUNT(*) FROM gentest_result x WHERE x.batch_id = b.id AND x.status = 'succeeded')
		FROM gentest_batch b ORDER BY b.created_at DESC, b.id DESC LIMIT ?`, limit)
}
