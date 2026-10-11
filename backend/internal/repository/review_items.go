package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// ListItems returns the review requests of the Project, newest first, each with its responses (in
// import order) and their comments (in the order the reviewer wrote them).
func (r *ReviewRepository) ListItems(ctx context.Context, projectID string) ([]domain.ReviewItem, error) {
	db := r.db.Reader()
	requests, err := listReviewRequests(ctx, db, projectID)
	if err != nil || len(requests) == 0 {
		return nil, err
	}
	responses, err := listReviewResponses(ctx, db, projectID)
	if err != nil {
		return nil, err
	}
	comments, err := listReviewComments(ctx, db, projectID)
	if err != nil {
		return nil, err
	}
	return assembleReviewItems(requests, responses, comments)
}

// listReviewRequests returns the Project's review requests, newest first.
func listReviewRequests(ctx context.Context, q queryer, projectID string) ([]domain.ReviewRequest, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, project_id, export_id, note, created_at FROM review_request WHERE project_id = ? ORDER BY created_at DESC, rowid DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list review requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var requests []domain.ReviewRequest
	for rows.Next() {
		var (
			req     domain.ReviewRequest
			created string
		)
		if err := rows.Scan(&req.ID, &req.ProjectID, &req.ExportID, &req.Note, &created); err != nil {
			return nil, fmt.Errorf("scan review request: %w", err)
		}
		if req.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	return requests, rows.Err()
}

// listReviewResponses returns the responses imported for the Project's requests, in import order.
func listReviewResponses(ctx context.Context, q queryer, projectID string) ([]domain.ReviewResponseRecord, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT s.id, s.review_request_id, s.reviewer_name, s.decision, s.comment, s.created_at, s.imported_at
		   FROM review_response s JOIN review_request q ON q.id = s.review_request_id
		  WHERE q.project_id = ? ORDER BY s.imported_at, s.rowid`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list review responses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var responses []domain.ReviewResponseRecord
	for rows.Next() {
		var (
			s                   domain.ReviewResponseRecord
			decision            string
			created, importedAt string
		)
		if err := rows.Scan(&s.ID, &s.RequestID, &s.ReviewerName, &decision, &s.Comment, &created, &importedAt); err != nil {
			return nil, fmt.Errorf("scan review response: %w", err)
		}
		s.Decision = domain.ReviewDecision(decision)
		if s.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		if s.ImportedAt, err = ParseTime(importedAt); err != nil {
			return nil, err
		}
		responses = append(responses, s)
	}
	return responses, rows.Err()
}

// listReviewComments returns the comments of the Project's responses, in the order they were written.
func listReviewComments(ctx context.Context, q queryer, projectID string) ([]domain.ReviewComment, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT c.id, c.review_response_id, c.timecode_ms, c.body, c.resolved
		   FROM review_comment c JOIN review_response s ON s.id = c.review_response_id
		   JOIN review_request q ON q.id = s.review_request_id
		  WHERE q.project_id = ? ORDER BY c.rowid`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list review comments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var comments []domain.ReviewComment
	for rows.Next() {
		var (
			c        domain.ReviewComment
			tc       sql.NullInt64
			resolved int
		)
		if err := rows.Scan(&c.ID, &c.ResponseID, &tc, &c.Body, &resolved); err != nil {
			return nil, fmt.Errorf("scan review comment: %w", err)
		}
		c.Resolved = resolved == 1
		if tc.Valid {
			c.TimecodeMs = &tc.Int64
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// assembleReviewItems hangs each response under its request and each comment under its response,
// keeping the order of the three lists. A child whose parent is not in the lists is an error: the
// queries join the parents, so it means the data is inconsistent, and attaching it anywhere would
// show the owner somebody else's feedback.
func assembleReviewItems(requests []domain.ReviewRequest, responses []domain.ReviewResponseRecord, comments []domain.ReviewComment) ([]domain.ReviewItem, error) {
	items := make([]domain.ReviewItem, len(requests))
	requestAt := make(map[string]int, len(requests))
	for i, req := range requests {
		items[i] = domain.ReviewItem{Request: req}
		requestAt[req.ID] = i
	}
	type position struct{ item, response int }
	responseAt := make(map[string]position, len(responses))
	for _, s := range responses {
		i, ok := requestAt[s.RequestID]
		if !ok {
			return nil, fmt.Errorf("review response %s: request %s is not in the Project", s.ID, s.RequestID)
		}
		responseAt[s.ID] = position{i, len(items[i].Responses)}
		items[i].Responses = append(items[i].Responses, s)
	}
	for _, c := range comments {
		pos, ok := responseAt[c.ResponseID]
		if !ok {
			return nil, fmt.Errorf("review comment %s: response %s is not in the Project", c.ID, c.ResponseID)
		}
		resp := &items[pos.item].Responses[pos.response]
		resp.Comments = append(resp.Comments, c)
	}
	return items, nil
}
