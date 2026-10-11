// Package review is the repository of Team Collaboration (spec/spec.md §27.1). It sits beside the
// per-aggregate repositories of the parent package and uses its DB and its error values.
package review

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

type (
	queryer interface {
		QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	}
	rowScanner interface{ Scan(dest ...any) error }
)

// Repository persists Team Collaboration (spec/spec.md §27.1): on the owner's side the Review
// Packages made and the Review Responses imported for them, on the reviewer's side the packages
// imported and what the reviewer wrote. All writes go through DB.Write.
type Repository struct {
	db  *repository.DB
	now func() time.Time
}

// New returns a Repository backed by db.
func New(db *repository.DB) *Repository { return &Repository{db: db, now: time.Now} }

// CreateRequest records a Review Package made for an Export of the Project.
func (r *Repository) CreateRequest(ctx context.Context, q domain.ReviewRequest) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO review_request (id, project_id, export_id, note, created_at) VALUES (?, ?, ?, ?, ?)`,
			q.ID, q.ProjectID, q.ExportID, q.Note, repository.FormatTime(q.CreatedAt)); err != nil {
			return fmt.Errorf("insert review request: %w", err)
		}
		return nil
	})
}

// GetRequest returns the review request of the Project (repository.ErrNotFound if it is another Project's).
func (r *Repository) GetRequest(ctx context.Context, projectID, id string) (domain.ReviewRequest, error) {
	var (
		q       domain.ReviewRequest
		created string
	)
	err := r.db.Reader().QueryRowContext(ctx,
		`SELECT id, project_id, export_id, note, created_at FROM review_request WHERE project_id = ? AND id = ?`, projectID, id).
		Scan(&q.ID, &q.ProjectID, &q.ExportID, &q.Note, &created)
	if err != nil {
		return q, repository.NotFoundOr(err, "get review request")
	}
	q.CreatedAt, err = repository.ParseTime(created)
	return q, err
}

// ImportResponse stores a Review Response for a review request of the Project. It is repository.ErrNotFound when
// the response is for no request of this Project, and repository.ErrConflict when the response (its response_id)
// was imported already. Nothing is written in either case.
func (r *Repository) ImportResponse(ctx context.Context, projectID string, resp domain.ReviewResponse) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := repository.Exists(ctx, tx, `SELECT 1 FROM review_request WHERE id = ? AND project_id = ?`, resp.ReviewID, projectID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM review_response WHERE id = ?`, resp.ResponseID).Scan(&n); err != nil {
			return fmt.Errorf("check review response: %w", err)
		}
		if n > 0 {
			return repository.ErrConflict
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO review_response (id, review_request_id, reviewer_name, decision, comment, created_at, imported_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			resp.ResponseID, resp.ReviewID, resp.ReviewerName, string(resp.Decision), resp.Comment, repository.FormatTime(resp.CreatedAt), repository.FormatTime(r.now())); err != nil {
			return fmt.Errorf("insert review response: %w", err)
		}
		for _, c := range resp.Comments {
			var tc any
			if c.TimecodeMs != nil {
				tc = *c.TimecodeMs
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO review_comment (id, review_response_id, timecode_ms, body) VALUES (?, ?, ?, ?)`,
				domain.NewID(), resp.ResponseID, tc, c.Body); err != nil {
				return fmt.Errorf("insert review comment: %w", err)
			}
		}
		return nil
	})
}

// SetCommentResolved marks a comment of the Project's review responses as resolved or open
// (repository.ErrNotFound for a comment of another Project).
func (r *Repository) SetCommentResolved(ctx context.Context, projectID, commentID string, resolved bool) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		v := 0
		if resolved {
			v = 1
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE review_comment SET resolved = ? WHERE id = ? AND review_response_id IN (
			   SELECT s.id FROM review_response s JOIN review_request q ON q.id = s.review_request_id WHERE q.project_id = ?)`,
			v, commentID, projectID)
		if err != nil {
			return fmt.Errorf("update review comment: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return repository.ErrNotFound
		}
		return nil
	})
}

const inboxSelect = `SELECT id, review_id, project_name, note, video_path, file_name, resolution, aspect_ratio, codec,
	reviewer_name, decision, comment, response_id, created_at, updated_at FROM review_inbox`

func scanInbox(row rowScanner) (domain.ReviewInbox, error) {
	var (
		i                domain.ReviewInbox
		decision         sql.NullString
		created, updated string
	)
	if err := row.Scan(&i.ID, &i.ReviewID, &i.ProjectName, &i.Note, &i.VideoPath, &i.Video.FileName, &i.Video.Resolution,
		&i.Video.AspectRatio, &i.Video.Codec, &i.ReviewerName, &decision, &i.Comment, &i.ResponseID, &created, &updated); err != nil {
		return i, err
	}
	i.Decision = domain.ReviewDecision(decision.String)
	var err error
	if i.CreatedAt, err = repository.ParseTime(created); err != nil {
		return i, err
	}
	i.UpdatedAt, err = repository.ParseTime(updated)
	return i, err
}

func readInbox(ctx context.Context, q queryer, id string) (domain.ReviewInbox, error) {
	i, err := scanInbox(q.QueryRowContext(ctx, inboxSelect+` WHERE id = ?`, id))
	if err != nil {
		return i, repository.NotFoundOr(err, "get review inbox")
	}
	rows, err := q.QueryContext(ctx, `SELECT id, timecode_ms, body, created_at FROM review_inbox_comment WHERE inbox_id = ? ORDER BY rowid`, id)
	if err != nil {
		return i, fmt.Errorf("list review inbox comments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			c       domain.ReviewInboxComment
			tc      sql.NullInt64
			created string
		)
		if err := rows.Scan(&c.ID, &tc, &c.Body, &created); err != nil {
			return i, fmt.Errorf("scan review inbox comment: %w", err)
		}
		if tc.Valid {
			c.TimecodeMs = &tc.Int64
		}
		if c.CreatedAt, err = repository.ParseTime(created); err != nil {
			return i, err
		}
		i.Comments = append(i.Comments, c)
	}
	return i, rows.Err()
}

// GetInbox returns the imported package with the reviewer's comments (repository.ErrNotFound if unknown).
func (r *Repository) GetInbox(ctx context.Context, id string) (domain.ReviewInbox, error) {
	return readInbox(ctx, r.db.Reader(), id)
}

// ListInbox returns the imported packages, newest first, without their comments.
func (r *Repository) ListInbox(ctx context.Context) ([]domain.ReviewInbox, error) {
	rows, err := r.db.Reader().QueryContext(ctx, inboxSelect+` ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return nil, fmt.Errorf("list review inbox: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ReviewInbox
	for rows.Next() {
		i, err := scanInbox(rows)
		if err != nil {
			return nil, fmt.Errorf("scan review inbox: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// CreateInbox stores an imported package. It is repository.ErrConflict when a package with the same review_id
// was imported already.
func (r *Repository) CreateInbox(ctx context.Context, i domain.ReviewInbox) error {
	return r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM review_inbox WHERE review_id = ?`, i.ReviewID).Scan(&n); err != nil {
			return fmt.Errorf("check review inbox: %w", err)
		}
		if n > 0 {
			return repository.ErrConflict
		}
		now := repository.FormatTime(r.now())
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO review_inbox (id, review_id, project_name, note, video_path, file_name, resolution, aspect_ratio, codec, response_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			i.ID, i.ReviewID, i.ProjectName, i.Note, i.VideoPath, i.Video.FileName, i.Video.Resolution, i.Video.AspectRatio, i.Video.Codec,
			i.ResponseID, now, now); err != nil {
			return fmt.Errorf("insert review inbox: %w", err)
		}
		return nil
	})
}

// touchInbox gives the inbox item a new response_id: its answer changed, so the file written from it
// is a different response.
func (r *Repository) touchInbox(ctx context.Context, tx *sql.Tx, id string) error {
	res, err := tx.ExecContext(ctx, `UPDATE review_inbox SET response_id = ?, updated_at = ? WHERE id = ?`, domain.NewID(), repository.FormatTime(r.now()), id)
	if err != nil {
		return fmt.Errorf("update review inbox: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// AddInboxComment adds a comment unless the item already has domain.MaxReviewComments of them
// (repository.ErrConflict), and returns the updated item.
func (r *Repository) AddInboxComment(ctx context.Context, inboxID string, c domain.ReviewCommentInput) (domain.ReviewInbox, error) {
	var out domain.ReviewInbox
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := repository.Exists(ctx, tx, `SELECT 1 FROM review_inbox WHERE id = ?`, inboxID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM review_inbox_comment WHERE inbox_id = ?`, inboxID).Scan(&n); err != nil {
			return fmt.Errorf("count review inbox comments: %w", err)
		}
		if n >= domain.MaxReviewComments {
			return repository.ErrConflict
		}
		var tc any
		if c.TimecodeMs != nil {
			tc = *c.TimecodeMs
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO review_inbox_comment (id, inbox_id, timecode_ms, body, created_at) VALUES (?, ?, ?, ?, ?)`,
			domain.NewID(), inboxID, tc, c.Body, repository.FormatTime(r.now())); err != nil {
			return fmt.Errorf("insert review inbox comment: %w", err)
		}
		if err := r.touchInbox(ctx, tx, inboxID); err != nil {
			return err
		}
		var err error
		out, err = readInbox(ctx, tx, inboxID)
		return err
	})
	return out, err
}

// DeleteInboxComment removes a comment of the item (repository.ErrNotFound if it is not the item's) and
// returns the updated item.
func (r *Repository) DeleteInboxComment(ctx context.Context, inboxID, commentID string) (domain.ReviewInbox, error) {
	var out domain.ReviewInbox
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM review_inbox_comment WHERE id = ? AND inbox_id = ?`, commentID, inboxID)
		if err != nil {
			return fmt.Errorf("delete review inbox comment: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return repository.ErrNotFound
		}
		if err := r.touchInbox(ctx, tx, inboxID); err != nil {
			return err
		}
		out, err = readInbox(ctx, tx, inboxID)
		return err
	})
	return out, err
}

// SetDecision writes the reviewer's name, decision and overall comment, and returns the updated item.
func (r *Repository) SetDecision(ctx context.Context, inboxID, reviewerName string, decision domain.ReviewDecision, comment string) (domain.ReviewInbox, error) {
	var out domain.ReviewInbox
	err := r.db.Write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE review_inbox SET reviewer_name = ?, decision = ?, comment = ? WHERE id = ?`,
			reviewerName, string(decision), comment, inboxID)
		if err != nil {
			return fmt.Errorf("update review decision: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return repository.ErrNotFound
		}
		if err := r.touchInbox(ctx, tx, inboxID); err != nil {
			return err
		}
		out, err = readInbox(ctx, tx, inboxID)
		return err
	})
	return out, err
}
