package domain

import (
	"strings"
	"time"
)

// The records of a review kept by this instance: the request the owner made, the responses that came back and the inbox of a reviewer
// (moved out of review.go, which holds the file formats; spec/spec.md §27.1).

// ReviewRequest is a Review Package the owner made: the Export it carries and the owner's note.
type ReviewRequest struct {
	ID        string
	ProjectID string
	ExportID  string
	Note      string
	CreatedAt time.Time
}

// ReviewComment is a comment of an imported Review Response. Resolved is the owner's mark that it
// has been dealt with.
type ReviewComment struct {
	ID         string
	ResponseID string
	TimecodeMs *int64
	Body       string
	Resolved   bool
}

// ReviewResponseRecord is an imported Review Response.
type ReviewResponseRecord struct {
	ID           string
	RequestID    string
	ReviewerName string
	Decision     ReviewDecision
	Comment      string
	CreatedAt    time.Time
	ImportedAt   time.Time
	Comments     []ReviewComment
}

// ReviewItem is a review request with the responses that came back, in the order they were imported.
type ReviewItem struct {
	Request   ReviewRequest
	Responses []ReviewResponseRecord
}

// Status is where the request stands: pending without a response, changes_requested when the latest
// response of any reviewer asks for changes, approved when every reviewer's latest response approves.
// A reviewer is told apart by the name they gave, ignoring surrounding whitespace. A reviewer's latest
// response is the one written last (the greatest CreatedAt, so a v1 imported after the v2 does not win;
// the one imported last among equals). Approval tells the owner something; it does not gate the Project.
func (i ReviewItem) Status() ReviewStatus {
	latest := map[string]ReviewResponseRecord{}
	for _, r := range i.Responses {
		name := strings.TrimSpace(r.ReviewerName)
		if prev, ok := latest[name]; ok && r.CreatedAt.Before(prev.CreatedAt) {
			continue
		}
		latest[name] = r
	}
	if len(latest) == 0 {
		return ReviewPending
	}
	for _, r := range latest {
		if r.Decision == DecisionChangesRequested {
			return ReviewChangesRequested
		}
	}
	return ReviewApproved
}

// OpenComments counts the comments of all responses that are not resolved.
func (i ReviewItem) OpenComments() int {
	n := 0
	for _, r := range i.Responses {
		for _, c := range r.Comments {
			if !c.Resolved {
				n++
			}
		}
	}
	return n
}

// ReviewInboxComment is a comment of the reviewer.
type ReviewInboxComment struct {
	ID         string
	TimecodeMs *int64
	Body       string
	CreatedAt  time.Time
}

// ReviewInbox is a Review Package a reviewer imported, with what the reviewer has written so far.
// VideoPath is relative to the assets directory. ResponseID changes with every change of the
// decision or the comments.
type ReviewInbox struct {
	ID           string
	ReviewID     string
	ProjectName  string
	Note         string
	VideoPath    string
	Video        ReviewVideo
	ReviewerName string
	Decision     ReviewDecision
	Comment      string
	ResponseID   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Comments     []ReviewInboxComment
}

// Response builds the Review Response file of the inbox item, or false while there is no decision.
func (i ReviewInbox) Response(now time.Time) (ReviewResponse, bool) {
	if !i.Decision.Valid() {
		return ReviewResponse{}, false
	}
	comments := make([]ReviewCommentInput, len(i.Comments))
	for n, c := range i.Comments {
		comments[n] = ReviewCommentInput{TimecodeMs: c.TimecodeMs, Body: c.Body}
	}
	return ReviewResponse{
		Format: ReviewResponseFormat, Version: ReviewVersion, ResponseID: i.ResponseID, ReviewID: i.ReviewID,
		ReviewerName: i.ReviewerName, Decision: i.Decision, Comment: i.Comment, Comments: comments, CreatedAt: now,
	}, true
}
