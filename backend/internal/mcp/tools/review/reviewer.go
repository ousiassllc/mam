package review

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// registerReviewer adds the tools of the reviewer: the inbox of imported packages, the comments, the
// decision and the response file.
func registerReviewer(r *mcpkit.Registrar, svc *service.ReviewService) {
	mcpkit.Read(r, "list_review_inbox",
		"レビュアーとして取り込んだ Review Package の一覧（新しい順）/ Review Packages imported as a reviewer, newest first. 画面の GET /reviews と同じ内容。取り込みは review_package_import のアップロード。",
		listInbox(svc))
	mcpkit.Read(r, "get_review",
		"取り込んだ Review Package の中身（Project 名・オーナーのメモ・動画の情報・自分のコメントと判定）/ An imported package with the reviewer's comments and decision.",
		getReview(svc))
	mcpkit.Write(r, "add_review_comment",
		"レビュアーのコメントを足す。position は秒（83.5）か 分:秒（1:23）、省略すると動画全体へのコメント。本文は 1〜2000 文字、200 件まで / Add a reviewer comment.",
		addComment(svc))
	mcpkit.Destroy(r, "delete_review_comment",
		"レビュアーのコメントを削除する / Delete a reviewer comment.",
		deleteComment(svc), mcpkit.Idempotent())
	mcpkit.Write(r, "decide_review",
		"レビュアーの名前・判定（approved / changes_requested）・全体コメントを書く。書き直すと応答（response）の ID が変わる / Write the reviewer's name, decision and overall comment.",
		decide(svc), mcpkit.Idempotent())
	mcpkit.Read(r, "get_review_video_url",
		"取り込んだ Review Package の動画のダウンロード URL（15 分有効）/ Download URL of the video of an imported package.",
		videoURL(r))
	mcpkit.Read(r, "get_review_response_url",
		"オーナーに返す Review Response（JSON）のダウンロード URL（15 分有効）。判定を書いてからでないと conflict / Download URL of the Review Response file to hand back to the owner.",
		responseURL(r))
}

// InboxInput names an imported package.
type InboxInput struct {
	InboxID string `json:"inbox_id" jsonschema:"取り込んだパッケージの ID（list_review_inbox の id。依頼側の review_id とは別）"`
}

// InboxItem is an imported package.
type InboxItem struct {
	ID          string `json:"id" jsonschema:"inbox_id: ほかのレビュアーのツールに渡す ID"`
	ReviewID    string `json:"review_id" jsonschema:"オーナー側のレビュー依頼の ID"`
	ProjectName string `json:"project_name"`
	Note        string `json:"note,omitempty" jsonschema:"オーナーのメモ"`
	Video       Video  `json:"video"`
	// Decision is empty until the reviewer decides.
	ReviewerName string         `json:"reviewer_name,omitempty"`
	Decision     string         `json:"decision,omitempty" jsonschema:"approved or changes_requested（未判定なら空）"`
	Comment      string         `json:"comment,omitempty" jsonschema:"全体コメント"`
	Comments     []InboxComment `json:"comments,omitempty"`
	CanRespond   bool           `json:"can_respond" jsonschema:"判定が書いてあり、応答ファイルを書き出せるか"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// Video describes the video of a package.
type Video struct {
	FileName    string `json:"file_name"`
	Resolution  string `json:"resolution"`
	AspectRatio string `json:"aspect_ratio"`
	Codec       string `json:"codec"`
}

// InboxComment is a comment of the reviewer.
type InboxComment struct {
	ID         string    `json:"id" jsonschema:"delete_review_comment に渡す ID"`
	TimecodeMs *int64    `json:"timecode_ms,omitempty"`
	Timecode   string    `json:"timecode,omitempty" jsonschema:"分:秒の表示"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

func inboxItem(i domain.ReviewInbox) InboxItem {
	it := InboxItem{
		ID: i.ID, ReviewID: i.ReviewID, ProjectName: i.ProjectName, Note: i.Note,
		Video:        Video{FileName: i.Video.FileName, Resolution: i.Video.Resolution, AspectRatio: i.Video.AspectRatio, Codec: i.Video.Codec},
		ReviewerName: i.ReviewerName, Decision: string(i.Decision), Comment: i.Comment, CanRespond: i.Decision.Valid(),
		Comments: make([]InboxComment, 0, len(i.Comments)), CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
	}
	for _, c := range i.Comments {
		ic := InboxComment{ID: c.ID, TimecodeMs: c.TimecodeMs, Body: c.Body, CreatedAt: c.CreatedAt}
		if c.TimecodeMs != nil {
			ic.Timecode = domain.FormatReviewTimecode(*c.TimecodeMs)
		}
		it.Comments = append(it.Comments, ic)
	}
	return it
}

// InboxOutput is the packages the instance imported as a reviewer.
type InboxOutput struct {
	Reviews []InboxItem `json:"reviews"`
}

// NoInput has no field.
type NoInput struct{}

func listInbox(svc *service.ReviewService) mcpkit.Handler[NoInput, InboxOutput] {
	return func(ctx context.Context, _ NoInput) (InboxOutput, error) {
		list, err := svc.Inbox(ctx)
		if err != nil {
			return InboxOutput{}, err
		}
		out := InboxOutput{Reviews: make([]InboxItem, 0, len(list))}
		for _, i := range list {
			out.Reviews = append(out.Reviews, inboxItem(i))
		}
		return out, nil
	}
}

func getReview(svc *service.ReviewService) mcpkit.Handler[InboxInput, InboxItem] {
	return func(ctx context.Context, in InboxInput) (InboxItem, error) {
		i, err := svc.Get(ctx, in.InboxID)
		if err != nil {
			return InboxItem{}, err
		}
		return inboxItem(i), nil
	}
}

// AddCommentInput is a reviewer comment.
type AddCommentInput struct {
	InboxInput
	Position string `json:"position,omitempty" jsonschema:"動画の位置。秒（83.5）か 分:秒（1:23）。省略すると動画全体"`
	Body     string `json:"body" jsonschema:"コメント本文（1〜2000 文字）"`
}

func addComment(svc *service.ReviewService) mcpkit.Handler[AddCommentInput, InboxItem] {
	return func(ctx context.Context, in AddCommentInput) (InboxItem, error) {
		i, err := svc.AddComment(ctx, in.InboxID, in.Position, in.Body)
		if err != nil {
			return InboxItem{}, err
		}
		return inboxItem(i), nil
	}
}

// DeleteCommentInput names a reviewer comment.
type DeleteCommentInput struct {
	InboxInput
	CommentID string `json:"comment_id"`
}

func deleteComment(svc *service.ReviewService) mcpkit.Handler[DeleteCommentInput, InboxItem] {
	return func(ctx context.Context, in DeleteCommentInput) (InboxItem, error) {
		i, err := svc.DeleteComment(ctx, in.InboxID, in.CommentID)
		if err != nil {
			return InboxItem{}, err
		}
		return inboxItem(i), nil
	}
}

// DecideInput is the reviewer's decision.
type DecideInput struct {
	InboxInput
	ReviewerName string `json:"reviewer_name" jsonschema:"レビュアーの表示名（1〜50 文字）"`
	Decision     string `json:"decision" jsonschema:"approved（承認）or changes_requested（修正依頼）"`
	Comment      string `json:"comment,omitempty" jsonschema:"全体コメント（2000 文字まで）"`
}

func decide(svc *service.ReviewService) mcpkit.Handler[DecideInput, InboxItem] {
	return func(ctx context.Context, in DecideInput) (InboxItem, error) {
		i, err := svc.Decide(ctx, in.InboxID, in.ReviewerName, in.Decision, in.Comment)
		if err != nil {
			return InboxItem{}, err
		}
		return inboxItem(i), nil
	}
}

func videoURL(r *mcpkit.Registrar) mcpkit.Handler[InboxInput, mcpkit.Link] {
	return func(ctx context.Context, in InboxInput) (mcpkit.Link, error) {
		return r.Files.DownloadURL(ctx, KindReviewVideo, in.InboxID)
	}
}

func responseURL(r *mcpkit.Registrar) mcpkit.Handler[InboxInput, mcpkit.Link] {
	return func(ctx context.Context, in InboxInput) (mcpkit.Link, error) {
		return r.Files.DownloadURL(ctx, KindReviewResponse, in.InboxID)
	}
}
