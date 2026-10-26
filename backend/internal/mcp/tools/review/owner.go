// Package review holds the MCP tools of Team Collaboration (spec/spec.md §27.1): the owner's review
// requests, Review Packages and imported Review Responses, and the reviewer's inbox, comments and decision.
// Files are the only channel between the two, so the packages, videos and responses go through the file URLs.
package review

import (
	"context"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools, the upload purposes and the download kinds of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	registerFiles(r, svc.Review)
	registerReviewer(r, svc.Review)

	mcpkit.Read(r, "list_project_reviews",
		"Project のレビュー依頼の一覧（状態・取り込んだ応答とコメント）と、依頼できる書き出し（新しい順）/ Review requests of a Project with the responses imported, and the Exports a request can be made for. 画面の GET /projects/:id/reviews と同じ内容。",
		listProjectReviews(svc.Review))
	mcpkit.Write(r, "create_review_request",
		"書き出しからレビュー依頼を作る。メモは 1000 文字まで。書き出しのファイルが無いと conflict。パッケージは get_review_package_url で取る / Make a review request for an Export.",
		createRequest(svc.Review))
	mcpkit.Write(r, "set_review_comment_resolved",
		"取り込んだレビューコメントを対応済み / 未対応にする / Mark an imported review comment as resolved or open again.",
		setCommentResolved(svc.Review), mcpkit.Idempotent())
	mcpkit.Read(r, "get_review_package_url",
		"レビュー依頼の Review Package（manifest.json と動画の ZIP）のダウンロード URL（15 分有効）。Project の ID・ファイルの場所・キーは入らない / Download URL of the Review Package of a request.",
		packageURL(r))
}

// ProjectInput names a Project.
type ProjectInput struct {
	ProjectID string `json:"project_id"`
}

// ProjectReviewsOutput is the review screen of a Project.
type ProjectReviewsOutput struct {
	ProjectID string        `json:"project_id"`
	Reviews   []RequestItem `json:"reviews" jsonschema:"レビュー依頼（新しい順）"`
	Exports   []ExportItem  `json:"exports" jsonschema:"レビューを依頼できる書き出し（新しい順）"`
}

// RequestItem is a review request with the responses that came back.
type RequestItem struct {
	ID           string         `json:"id" jsonschema:"レビュー依頼の ID（review_id）"`
	ExportID     string         `json:"export_id"`
	Export       *ExportItem    `json:"export,omitempty"`
	Note         string         `json:"note,omitempty"`
	Status       string         `json:"status" jsonschema:"pending（未回答）, approved, changes_requested"`
	OpenComments int            `json:"open_comments" jsonschema:"対応済みでないコメントの数"`
	Responses    []ResponseItem `json:"responses"`
	CreatedAt    time.Time      `json:"created_at"`
}

// ExportItem is an Export a review can be requested for.
type ExportItem struct {
	ID          string    `json:"id"`
	Resolution  string    `json:"resolution"`
	AspectRatio string    `json:"aspect_ratio"`
	Codec       string    `json:"codec"`
	ExportedAt  time.Time `json:"exported_at"`
}

// ResponseItem is an imported Review Response.
type ResponseItem struct {
	ID           string        `json:"id"`
	ReviewerName string        `json:"reviewer_name"`
	Decision     string        `json:"decision" jsonschema:"approved or changes_requested"`
	Comment      string        `json:"comment,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	ImportedAt   time.Time     `json:"imported_at"`
	Comments     []CommentItem `json:"comments"`
}

// CommentItem is a comment of an imported response.
type CommentItem struct {
	ID         string `json:"id" jsonschema:"set_review_comment_resolved に渡す ID"`
	TimecodeMs *int64 `json:"timecode_ms,omitempty" jsonschema:"動画の位置（ミリ秒）。無ければ動画全体へのコメント"`
	Timecode   string `json:"timecode,omitempty" jsonschema:"分:秒の表示"`
	Body       string `json:"body"`
	Resolved   bool   `json:"resolved"`
}

func exportItem(e domain.Export) ExportItem {
	return ExportItem{ID: e.ID, Resolution: e.Resolution, AspectRatio: e.AspectRatio, Codec: e.Codec, ExportedAt: e.ExportedAt}
}

func requestItem(v service.ReviewItemView) RequestItem {
	it := RequestItem{
		ID: v.Request.ID, ExportID: v.Request.ExportID, Note: v.Request.Note, Status: string(v.Status()), OpenComments: v.OpenComments(),
		Responses: make([]ResponseItem, 0, len(v.Responses)), CreatedAt: v.Request.CreatedAt,
	}
	if v.Export.ID != "" {
		e := exportItem(v.Export)
		it.Export = &e
	}
	for _, r := range v.Responses {
		ri := ResponseItem{
			ID: r.ID, ReviewerName: r.ReviewerName, Decision: string(r.Decision), Comment: r.Comment,
			CreatedAt: r.CreatedAt, ImportedAt: r.ImportedAt, Comments: make([]CommentItem, 0, len(r.Comments)),
		}
		for _, c := range r.Comments {
			ci := CommentItem{ID: c.ID, TimecodeMs: c.TimecodeMs, Body: c.Body, Resolved: c.Resolved}
			if c.TimecodeMs != nil {
				ci.Timecode = domain.FormatReviewTimecode(*c.TimecodeMs)
			}
			ri.Comments = append(ri.Comments, ci)
		}
		it.Responses = append(it.Responses, ri)
	}
	return it
}

func projectReviewsOutput(v service.ReviewsView) ProjectReviewsOutput {
	out := ProjectReviewsOutput{ProjectID: v.Project.ID, Reviews: make([]RequestItem, 0, len(v.Items)), Exports: make([]ExportItem, 0, len(v.Exports))}
	for _, it := range v.Items {
		out.Reviews = append(out.Reviews, requestItem(it))
	}
	for _, e := range v.Exports {
		out.Exports = append(out.Exports, exportItem(e))
	}
	return out
}

func listProjectReviews(svc *service.ReviewService) mcpkit.Handler[ProjectInput, ProjectReviewsOutput] {
	return func(ctx context.Context, in ProjectInput) (ProjectReviewsOutput, error) {
		v, err := svc.ProjectReviews(ctx, in.ProjectID, "")
		if err != nil {
			return ProjectReviewsOutput{}, err
		}
		return projectReviewsOutput(v), nil
	}
}

// CreateInput is a review request to make.
type CreateInput struct {
	ProjectID string `json:"project_id"`
	ExportID  string `json:"export_id" jsonschema:"レビューを依頼する書き出し（list_project_reviews の exports[].id）"`
	Note      string `json:"note,omitempty" jsonschema:"レビュアーへのメモ（1000 文字まで）"`
}

// CreateOutput is the request that was made, with the review screen.
type CreateOutput struct {
	ReviewID string `json:"review_id" jsonschema:"作ったレビュー依頼の ID"`
	ProjectReviewsOutput
}

func createRequest(svc *service.ReviewService) mcpkit.Handler[CreateInput, CreateOutput] {
	return func(ctx context.Context, in CreateInput) (CreateOutput, error) {
		v, err := svc.CreateRequest(ctx, in.ProjectID, in.ExportID, in.Note)
		if err != nil {
			return CreateOutput{}, err
		}
		return CreateOutput{ReviewID: v.Touched, ProjectReviewsOutput: projectReviewsOutput(v)}, nil
	}
}

// ResolveInput marks one comment.
type ResolveInput struct {
	ProjectID string `json:"project_id"`
	CommentID string `json:"comment_id" jsonschema:"list_project_reviews の comments[].id"`
	Resolved  bool   `json:"resolved" jsonschema:"true で対応済み、false で未対応に戻す"`
}

func setCommentResolved(svc *service.ReviewService) mcpkit.Handler[ResolveInput, RequestItem] {
	return func(ctx context.Context, in ResolveInput) (RequestItem, error) {
		it, err := svc.SetCommentResolved(ctx, in.ProjectID, in.CommentID, in.Resolved)
		if err != nil {
			return RequestItem{}, err
		}
		return requestItem(it), nil
	}
}

// PackageInput names a review request.
type PackageInput struct {
	ProjectID string `json:"project_id"`
	ReviewID  string `json:"review_id" jsonschema:"レビュー依頼の ID"`
}

func packageURL(r *mcpkit.Registrar) mcpkit.Handler[PackageInput, mcpkit.Link] {
	return func(ctx context.Context, in PackageInput) (mcpkit.Link, error) {
		return r.Files.DownloadURL(ctx, KindReviewPackage, in.ProjectID+"/"+in.ReviewID)
	}
}

// splitID splits "<a>/<b>" of a download resource id.
func splitID(id string) (a, b string, err error) {
	a, b, ok := strings.Cut(id, "/")
	if !ok {
		return "", "", service.ErrNotFound
	}
	return a, b, nil
}
