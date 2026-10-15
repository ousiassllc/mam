package review_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/review"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
)

func ptr[T any](v T) *T { return &v }

func exec(t *testing.T, db *repository.DB, query string, args ...any) error {
	t.Helper()
	return db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	})
}

func seedProject(t *testing.T, db *repository.DB, id string) {
	t.Helper()
	require.NoError(t, exec(t, db,
		`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy)
		 VALUES (?, 'auto', 'key_check', 'product_cm', 'balanced')`, id))
}

func seedReviewRequest(t *testing.T, r *review.Repository, projectID, id, exportID string, created time.Time) {
	t.Helper()
	require.NoError(t, r.CreateRequest(context.Background(), domain.ReviewRequest{
		ID: id, ProjectID: projectID, ExportID: exportID, Note: "note " + id, CreatedAt: created,
	}))
}

func TestRepository_ListItems_OrdersAndAttachesChildren(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	r := review.New(db)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []string{"p1", "p2"} {
		seedProject(t, db, p)
		require.NoError(t, exec(t, db,
			`INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec) VALUES (?, ?, 'f.mp4', '1080p', '16:9', 'h264')`, "x-"+p, p))
	}

	// q1 is the oldest; q2 and q3 share a time, so the one made later (q3) comes first.
	seedReviewRequest(t, r, "p1", "q1", "x-p1", base)
	seedReviewRequest(t, r, "p1", "q2", "x-p1", base.Add(time.Hour))
	seedReviewRequest(t, r, "p1", "q3", "x-p1", base.Add(time.Hour))
	seedReviewRequest(t, r, "p2", "other", "x-p2", base.Add(2*time.Hour))

	imp := func(projectID, reviewID, responseID, reviewer string, decision domain.ReviewDecision, comments ...domain.ReviewCommentInput) {
		t.Helper()
		require.NoError(t, r.ImportResponse(ctx, projectID, domain.ReviewResponse{
			ResponseID: responseID, ReviewID: reviewID, ReviewerName: reviewer, Decision: decision,
			Comment: "overall " + responseID, Comments: comments, CreatedAt: base,
		}))
	}
	imp("p1", "q1", "s1", "Ann", domain.DecisionChangesRequested,
		domain.ReviewCommentInput{TimecodeMs: ptr(int64(1500)), Body: "s1-a"},
		domain.ReviewCommentInput{Body: "s1-b"})
	imp("p1", "q3", "s3", "Bob", domain.DecisionApproved)
	imp("p1", "q1", "s2", "Bob", domain.DecisionApproved,
		domain.ReviewCommentInput{TimecodeMs: ptr(int64(0)), Body: "s2-a"})
	imp("p2", "other", "so", "Cy", domain.DecisionApproved,
		domain.ReviewCommentInput{Body: "so-a"})
	// A comment added to s1 after s2's were written comes after them in the global order but
	// must still hang under s1.
	require.NoError(t, exec(t, db,
		`INSERT INTO review_comment (id, review_response_id, body, resolved) VALUES ('late', 's1', 's1-late', 1)`))

	items, err := r.ListItems(ctx, "p1")
	require.NoError(t, err)

	var ids []string
	for _, it := range items {
		ids = append(ids, it.Request.ID)
	}
	assert.Equal(t, []string{"q3", "q2", "q1"}, ids, "requests are newest first")
	assert.Equal(t, "x-p1", items[0].Request.ExportID)
	assert.Equal(t, "note q3", items[0].Request.Note)
	assert.True(t, items[2].Request.CreatedAt.Equal(base))

	require.Len(t, items[0].Responses, 1)
	assert.Equal(t, "s3", items[0].Responses[0].ID)
	assert.Equal(t, "q3", items[0].Responses[0].RequestID)
	assert.Empty(t, items[1].Responses)

	q1 := items[2].Responses
	require.Len(t, q1, 2, "responses of another request or Project are not mixed in")
	assert.Equal(t, "s1", q1[0].ID, "responses are in import order")
	assert.Equal(t, "s2", q1[1].ID)
	assert.Equal(t, domain.DecisionChangesRequested, q1[0].Decision)
	assert.Equal(t, "Ann", q1[0].ReviewerName)
	assert.Equal(t, "overall s1", q1[0].Comment)

	require.Len(t, q1[0].Comments, 3)
	bodies := func(cs []domain.ReviewComment) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Body)
		}
		return out
	}
	assert.Equal(t, []string{"s1-a", "s1-b", "s1-late"}, bodies(q1[0].Comments), "comments are in the order they were written")
	require.NotNil(t, q1[0].Comments[0].TimecodeMs)
	assert.Equal(t, int64(1500), *q1[0].Comments[0].TimecodeMs)
	assert.Nil(t, q1[0].Comments[1].TimecodeMs)
	assert.False(t, q1[0].Comments[0].Resolved)
	assert.True(t, q1[0].Comments[2].Resolved)
	assert.Equal(t, "s1", q1[0].Comments[2].ResponseID)
	assert.Equal(t, []string{"s2-a"}, bodies(q1[1].Comments))
	require.NotNil(t, q1[1].Comments[0].TimecodeMs)
	assert.Equal(t, int64(0), *q1[1].Comments[0].TimecodeMs)
	assert.Empty(t, items[0].Responses[0].Comments)
}

func TestRepository_ListItems_NoRequests(t *testing.T) {
	db := testdb.Open(t)
	seedProject(t, db, "p1")
	items, err := review.New(db).ListItems(context.Background(), "p1")
	require.NoError(t, err)
	assert.Empty(t, items)
}
