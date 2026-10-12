package domain

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Team Collaboration (spec/spec.md §27.1): review and approval by handing files over. The owner
// (who has the Project) makes a Review Package from one Export; a reviewer imports it into an
// instance of their own and writes a Review Response, which the owner imports. There are no
// accounts and no shared state; the reviewer is identified by a name they typed, which nothing checks.
//
// What is shared is only what the structs below hold, so nothing else of a Project (the Brief, the
// chat, the reference images, the faces, the API keys) can be handed over by accident.
const (
	ReviewPackageFormat  = "mam-review-package"
	ReviewResponseFormat = "mam-review-response"
	ReviewVersion        = 1
	// ReviewManifestName is the manifest entry of the package; the video entry is ReviewVideoName.
	ReviewManifestName = "manifest.json"

	MaxReviewNoteRunes     = 1000
	MaxReviewerNameRunes   = 50
	MaxReviewCommentRunes  = 2000
	MaxReviewComments      = 200
	MaxReviewTimecodeMs    = int64(24 * time.Hour / time.Millisecond)
	maxReviewMetaRunes     = 100
	maxReviewProjectRunes  = 200
	reviewVideoNameMP4     = "video.mp4"
	reviewVideoNameWebM    = "video.webm"
	reviewVideoNamePattern = "video.mp4 / video.webm"
)

// ReviewDecision is the verdict of a reviewer.
type ReviewDecision string

// Review decisions.
const (
	DecisionApproved         ReviewDecision = "approved"
	DecisionChangesRequested ReviewDecision = "changes_requested"
)

// Valid reports whether d is a known decision.
func (d ReviewDecision) Valid() bool { return d == DecisionApproved || d == DecisionChangesRequested }

// Label is the decision as shown to the user.
func (d ReviewDecision) Label() string {
	switch d {
	case DecisionApproved:
		return "承認"
	case DecisionChangesRequested:
		return "修正依頼"
	}
	return "未回答"
}

// ReviewStatus is where a review request stands.
type ReviewStatus string

// Review statuses.
const (
	ReviewPending          ReviewStatus = "pending"
	ReviewApproved         ReviewStatus = "approved"
	ReviewChangesRequested ReviewStatus = "changes_requested"
)

// Label is the status as shown to the user.
func (s ReviewStatus) Label() string {
	switch s {
	case ReviewApproved:
		return "承認済み"
	case ReviewChangesRequested:
		return "修正依頼あり"
	}
	return "未回答"
}

// ReviewVideoName is the fixed entry name of the video in a package for the codec: the original path
// is never part of what is shared.
func ReviewVideoName(codec string) string {
	if ExportExtension(codec) == "webm" {
		return reviewVideoNameWebM
	}
	return reviewVideoNameMP4
}

// ReviewVideoMime is the media type of the package video called name.
func ReviewVideoMime(name string) string {
	if name == reviewVideoNameWebM {
		return "video/webm"
	}
	return "video/mp4"
}

// ReviewVideo describes the video of a package.
type ReviewVideo struct {
	FileName    string `json:"file_name"`
	Resolution  string `json:"resolution"`
	AspectRatio string `json:"aspect_ratio"`
	Codec       string `json:"codec"`
}

// ReviewManifest is manifest.json of a Review Package.
type ReviewManifest struct {
	Format      string      `json:"format"`
	Version     int         `json:"version"`
	ReviewID    string      `json:"review_id"`
	ProjectName string      `json:"project_name"`
	Note        string      `json:"note"`
	ExportedAt  time.Time   `json:"exported_at"`
	Video       ReviewVideo `json:"video"`
}

// ErrInvalidReview means a Review Package or Review Response cannot be used.
var ErrInvalidReview = errors.New("domain: invalid review file")

func invalidReview(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidReview, fmt.Sprintf(format, args...))
}

func tooLong(s string, limit int) bool { return utf8.RuneCountInString(s) > limit }

// Validate checks a manifest read from a file somebody else made.
func (m ReviewManifest) Validate() error {
	switch {
	case m.Format != ReviewPackageFormat:
		return invalidReview("MAM のレビューパッケージではありません")
	case m.Version != ReviewVersion:
		return invalidReview("対応していないパッケージのバージョンです（%d）", m.Version)
	case !validReviewID(m.ReviewID):
		return invalidReview("review_id が正しくありません")
	case strings.TrimSpace(m.ProjectName) == "" || tooLong(m.ProjectName, maxReviewProjectRunes):
		return invalidReview("project_name が正しくありません")
	case tooLong(m.Note, MaxReviewNoteRunes):
		return invalidReview("note が長すぎます")
	case m.Video.FileName != reviewVideoNameMP4 && m.Video.FileName != reviewVideoNameWebM:
		return invalidReview("video.file_name は %s のいずれかです", reviewVideoNamePattern)
	case tooLong(m.Video.Resolution, maxReviewMetaRunes) || tooLong(m.Video.AspectRatio, maxReviewMetaRunes) || tooLong(m.Video.Codec, maxReviewMetaRunes):
		return invalidReview("video の項目が長すぎます")
	}
	return nil
}

var reviewIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validReviewID reports whether id can be stored and shown: a short token of letters, digits, "-" and "_".
func validReviewID(id string) bool { return reviewIDPattern.MatchString(id) }

// HasReviewVideoSignature reports whether head (the first bytes of the file) is the container that
// the entry name says: an MP4 has "ftyp" at offset 4, a WebM starts with the EBML header.
func HasReviewVideoSignature(name string, head []byte) bool {
	if name == reviewVideoNameWebM {
		return bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3})
	}
	return len(head) >= 8 && string(head[4:8]) == "ftyp"
}

// ReviewCommentInput is a comment of a Review Response. TimecodeMs is where in the video it is
// about (nil: the video as a whole).
type ReviewCommentInput struct {
	TimecodeMs *int64 `json:"timecode_ms,omitempty"`
	Body       string `json:"body"`
}

// Validate checks one comment.
func (c ReviewCommentInput) Validate() error {
	if c.TimecodeMs != nil && (*c.TimecodeMs < 0 || *c.TimecodeMs > MaxReviewTimecodeMs) {
		return invalidReview("コメントの位置が範囲外です")
	}
	if strings.TrimSpace(c.Body) == "" || tooLong(c.Body, MaxReviewCommentRunes) {
		return invalidReview("コメントは 1〜%d 文字で書いてください", MaxReviewCommentRunes)
	}
	return nil
}

// ReviewResponse is the file a reviewer returns to the owner.
type ReviewResponse struct {
	Format       string               `json:"format"`
	Version      int                  `json:"version"`
	ResponseID   string               `json:"response_id"`
	ReviewID     string               `json:"review_id"`
	ReviewerName string               `json:"reviewer_name"`
	Decision     ReviewDecision       `json:"decision"`
	Comment      string               `json:"comment"`
	Comments     []ReviewCommentInput `json:"comments"`
	CreatedAt    time.Time            `json:"created_at"`
}

// Validate checks a response read from a file somebody else made.
func (r ReviewResponse) Validate() error {
	switch {
	case r.Format != ReviewResponseFormat:
		return invalidReview("MAM のレビュー応答ではありません")
	case r.Version != ReviewVersion:
		return invalidReview("対応していない応答のバージョンです（%d）", r.Version)
	case !validReviewID(r.ResponseID) || !validReviewID(r.ReviewID):
		return invalidReview("response_id / review_id が正しくありません")
	case !r.Decision.Valid():
		return invalidReview("decision が正しくありません")
	case tooLong(r.Comment, MaxReviewCommentRunes):
		return invalidReview("comment が長すぎます")
	case len(r.Comments) > MaxReviewComments:
		return invalidReview("コメントは %d 件までです", MaxReviewComments)
	}
	if err := ValidateReviewerName(r.ReviewerName); err != nil {
		return err
	}
	for _, c := range r.Comments {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateReviewerName checks the name a reviewer gives (1-50 characters, no control characters).
func ValidateReviewerName(name string) error {
	if strings.TrimSpace(name) == "" || tooLong(name, MaxReviewerNameRunes) || strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return invalidReview("表示名は 1〜%d 文字で入力してください", MaxReviewerNameRunes)
	}
	return nil
}

var reviewTimecodePattern = regexp.MustCompile(`^(?:(\d{1,4}):)?(\d{1,9}(?:\.\d{1,3})?)$`)

// ParseReviewTimecode reads the position a reviewer typed: seconds ("83.5") or minutes and seconds
// ("1:23"). Empty means the video as a whole (nil).
func ParseReviewTimecode(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil //nolint:nilnil // nil is "no position"
	}
	m := reviewTimecodePattern.FindStringSubmatch(s)
	if m == nil {
		return nil, invalidReview("位置は秒（83.5）か 分:秒（1:23）で入力してください")
	}
	minutes, _ := strconv.ParseInt(m[1], 10, 64) // empty when there are no minutes: 0
	seconds, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return nil, invalidReview("位置は秒（83.5）か 分:秒（1:23）で入力してください")
	}
	ms := minutes*60_000 + int64(math.Round(seconds*1000))
	if ms > MaxReviewTimecodeMs {
		return nil, invalidReview("位置は 24 時間以内で入力してください")
	}
	return &ms, nil
}

// FormatReviewTimecode shows a position as minutes:seconds.
func FormatReviewTimecode(ms int64) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

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
// response is the one written last (the greatest CreatedAt, so a v1 imported after the v2 does not win);
// among responses written at the same time the one imported last is the latest. Approval tells the
// owner something; it does not gate the Project.
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
