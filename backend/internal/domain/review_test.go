package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewItem_StatusFollowsTheLatestAnswerOfEveryReviewer(t *testing.T) {
	resp := func(name string, d ReviewDecision) ReviewResponseRecord {
		return ReviewResponseRecord{ReviewerName: name, Decision: d}
	}
	tests := []struct {
		name      string
		responses []ReviewResponseRecord
		want      ReviewStatus
	}{
		{"no response", nil, ReviewPending},
		{"one approval", []ReviewResponseRecord{resp("A", DecisionApproved)}, ReviewApproved},
		{"one change request", []ReviewResponseRecord{resp("A", DecisionChangesRequested)}, ReviewChangesRequested},
		{"everybody approves", []ReviewResponseRecord{resp("A", DecisionApproved), resp("B", DecisionApproved)}, ReviewApproved},
		{"one of two asks for changes", []ReviewResponseRecord{resp("A", DecisionApproved), resp("B", DecisionChangesRequested)}, ReviewChangesRequested},
		{"a later approval replaces the request", []ReviewResponseRecord{resp("A", DecisionChangesRequested), resp("A", DecisionApproved)}, ReviewApproved},
		{"a later request replaces the approval", []ReviewResponseRecord{resp("A", DecisionApproved), resp("A", DecisionChangesRequested)}, ReviewChangesRequested},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ReviewItem{Responses: tt.responses}.Status())
		})
	}
}

func TestParseReviewTimecode(t *testing.T) {
	ms := func(v int64) *int64 { return &v }
	ok := map[string]*int64{
		"": nil, "  ": nil, "0": ms(0), "83.5": ms(83500), "1:23": ms(83000), "1:23.25": ms(83250), "10:00": ms(600000), "0.001": ms(1),
	}
	for in, want := range ok {
		got, err := ParseReviewTimecode(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"abc", "-1", "1e3", "0x10", "1:2:3", ":30", "1:", "1.2345", "+5", "1_0", "25:00:00", "1440:01", "999999999999"} {
		_, err := ParseReviewTimecode(in)
		assert.ErrorIs(t, err, ErrInvalidReview, in)
	}
}

func TestReviewResponse_ValidateAcceptsOnlyAWellFormedAnswer(t *testing.T) {
	good := func() ReviewResponse {
		return ReviewResponse{
			Format: ReviewResponseFormat, Version: ReviewVersion, ResponseID: "r-1", ReviewID: "q_1", ReviewerName: "Hana",
			Decision: DecisionApproved, CreatedAt: time.Now(),
		}
	}
	require.NoError(t, good().Validate())
	tc := int64(500)
	withComment := good()
	withComment.Comments = []ReviewCommentInput{{TimecodeMs: &tc, Body: "ok"}, {Body: "whole"}}
	require.NoError(t, withComment.Validate())

	bad := map[string]func(*ReviewResponse){
		"format":       func(r *ReviewResponse) { r.Format = "x" },
		"version":      func(r *ReviewResponse) { r.Version = 2 },
		"response id":  func(r *ReviewResponse) { r.ResponseID = "../x" },
		"review id":    func(r *ReviewResponse) { r.ReviewID = "" },
		"decision":     func(r *ReviewResponse) { r.Decision = "maybe" },
		"name blank":   func(r *ReviewResponse) { r.ReviewerName = " " },
		"name control": func(r *ReviewResponse) { r.ReviewerName = "a\nb" },
		"name long":    func(r *ReviewResponse) { r.ReviewerName = strings.Repeat("あ", 51) },
		"comment long": func(r *ReviewResponse) { r.Comment = strings.Repeat("a", 2001) },
		"too many":     func(r *ReviewResponse) { r.Comments = make([]ReviewCommentInput, 201) },
		"empty body":   func(r *ReviewResponse) { r.Comments = []ReviewCommentInput{{Body: ""}} },
		"position": func(r *ReviewResponse) {
			neg := int64(-1)
			r.Comments = []ReviewCommentInput{{TimecodeMs: &neg, Body: "x"}}
		},
		"position days": func(r *ReviewResponse) {
			d := MaxReviewTimecodeMs + 1
			r.Comments = []ReviewCommentInput{{TimecodeMs: &d, Body: "x"}}
		},
	}
	for name, mutate := range bad {
		r := good()
		mutate(&r)
		assert.ErrorIs(t, r.Validate(), ErrInvalidReview, name)
	}
}

func TestHasReviewVideoSignature(t *testing.T) {
	assert.True(t, HasReviewVideoSignature("video.mp4", []byte("\x00\x00\x00\x18ftypisom")))
	assert.False(t, HasReviewVideoSignature("video.mp4", []byte("<html>")))
	assert.False(t, HasReviewVideoSignature("video.mp4", []byte("ftyp")), "ftyp must be at offset 4")
	assert.True(t, HasReviewVideoSignature("video.webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01}))
	assert.False(t, HasReviewVideoSignature("video.webm", []byte("\x00\x00\x00\x18ftypisom")))
}

func TestReviewInbox_ResponseNeedsADecisionAndCarriesOnlyTheAnswer(t *testing.T) {
	i := ReviewInbox{ReviewID: "q1", ResponseID: "r1", ProjectName: "Secret Project", Note: "owner's note", VideoPath: "reviews/x/video.mp4"}
	_, ok := i.Response(time.Now())
	assert.False(t, ok, "no decision yet")

	tc := int64(1000)
	i.ReviewerName, i.Decision, i.Comment = "Taro", DecisionChangesRequested, "fix"
	i.Comments = []ReviewInboxComment{{ID: "c1", TimecodeMs: &tc, Body: "logo"}}
	resp, ok := i.Response(time.Now())
	require.True(t, ok)
	assert.Equal(t, ReviewResponse{
		Format: ReviewResponseFormat, Version: ReviewVersion, ResponseID: "r1", ReviewID: "q1", ReviewerName: "Taro",
		Decision: DecisionChangesRequested, Comment: "fix", Comments: []ReviewCommentInput{{TimecodeMs: &tc, Body: "logo"}}, CreatedAt: resp.CreatedAt,
	}, resp)
	require.NoError(t, resp.Validate())
}
