package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/bundlezip"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/bundlepath"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/review"
)

// Team Collaboration limits (spec/spec.md §27.1). The files come from other people, so what is read from them is bounded.
const (
	maxReviewVideoBytes    = 2 << 30 // the video of a package
	MaxReviewPackageBytes  = maxReviewVideoBytes + 1<<20
	maxReviewResponseBytes = 1 << 20 // a Review Response and manifest.json
	// MaxReviewResponseRequestBytes caps the upload of a Review Response: the file plus the form overhead.
	MaxReviewResponseRequestBytes = maxReviewResponseBytes + 1<<20
	reviewVideoSniffBytes         = 16
	reviewsDir                    = bundlepath.ReviewInboxDir + "/" // below the assets directory: the videos of imported packages
)

// ReviewService is Team Collaboration: the owner makes Review Packages and imports the responses; a
// reviewer imports a package, comments, decides and writes the response. Files are the only channel.
type ReviewService struct {
	*core
	reviews *review.Repository
	editors *repository.EditorRepository
	chatSvc *ChatService
	now     func() time.Time
}

// reviewInvalid turns the error of a review file check into a ValidationError.
func reviewInvalid(field string, err error) error {
	if errors.Is(err, domain.ErrInvalidReview) {
		return &ValidationError{Field: field, Message: strings.TrimPrefix(err.Error(), domain.ErrInvalidReview.Error()+": ")}
	}
	return err
}

// ReviewItemView is a review request with the Export it carries.
type ReviewItemView struct {
	domain.ReviewItem
	Export domain.Export
}

// ReviewsView is what the owner's review screen shows.
type ReviewsView struct {
	Project domain.Project
	Items   []ReviewItemView
	Exports []domain.Export // the Exports a review can be requested for, newest first
	Touched string          // the review request a create or an import changed ("" for neither)
}

// ProjectReviews returns the review screen of a Project; touched is the review request just changed.
func (s *ReviewService) ProjectReviews(ctx context.Context, projectID, touched string) (ReviewsView, error) {
	p, err := s.getProject(ctx, projectID)
	if err != nil {
		return ReviewsView{}, err
	}
	exports, err := s.editors.ListExports(ctx, projectID)
	if err != nil {
		return ReviewsView{}, err
	}
	byID := make(map[string]domain.Export, len(exports))
	for _, e := range exports {
		byID[e.ID] = e
	}
	items, err := s.reviews.ListItems(ctx, projectID)
	if err != nil {
		return ReviewsView{}, err
	}
	view := ReviewsView{Project: p, Exports: exports, Touched: touched}
	for _, it := range items {
		view.Items = append(view.Items, ReviewItemView{ReviewItem: it, Export: byID[it.Request.ExportID]})
	}
	return view, nil
}

// CreateRequest makes a review request for an Export of the Project (POST /projects/:id/reviews).
// The Export must belong to the Project (ErrNotFound) and its file must still exist (ErrConflict).
func (s *ReviewService) CreateRequest(ctx context.Context, projectID, exportID, note string) (ReviewsView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return ReviewsView{}, err
	}
	if len([]rune(note)) > domain.MaxReviewNoteRunes {
		return ReviewsView{}, invalid("note", "メモは %d 文字以内で入力してください", domain.MaxReviewNoteRunes)
	}
	e, err := s.editors.GetExport(ctx, projectID, exportID)
	if err != nil {
		return ReviewsView{}, notFound(err)
	}
	if !s.exportFileExists(e) {
		return ReviewsView{}, conflict("この書き出しのファイルが見つかりません")
	}
	q := domain.ReviewRequest{ID: domain.NewID(), ProjectID: projectID, ExportID: e.ID, Note: strings.TrimSpace(note), CreatedAt: s.now()}
	if err := s.reviews.CreateRequest(ctx, q); err != nil {
		return ReviewsView{}, err
	}
	return s.ProjectReviews(ctx, projectID, q.ID)
}

func (s *ReviewService) exportFileExists(e domain.Export) bool {
	if !bundlepath.ValidFilePath(e.ProjectID, e.FilePath) {
		return false
	}
	st, err := os.Stat(filepath.Join(s.assetsDir, filepath.FromSlash(e.FilePath)))
	return err == nil && st.Mode().IsRegular()
}

// ReviewPackage is a Review Package ready to be written. Write streams the archive, so the caller
// can still answer with an error status before the first byte is sent.
type ReviewPackage struct {
	// FileName is the suggested download name.
	FileName  string
	manifest  domain.ReviewManifest
	videoPath string
}

// Package prepares the Review Package of a review request of the Project
// (GET /projects/:id/reviews/:reviewId/package). ErrNotFound when the request is another Project's,
// ErrConflict when the video file is gone.
func (s *ReviewService) Package(ctx context.Context, projectID, reviewID string) (ReviewPackage, error) {
	p, err := s.getProject(ctx, projectID)
	if err != nil {
		return ReviewPackage{}, err
	}
	q, err := s.reviews.GetRequest(ctx, projectID, reviewID)
	if err != nil {
		return ReviewPackage{}, notFound(err)
	}
	e, err := s.editors.GetExport(ctx, projectID, q.ExportID)
	if err != nil {
		return ReviewPackage{}, notFound(err)
	}
	if !s.exportFileExists(e) {
		return ReviewPackage{}, conflict("この書き出しのファイルが見つかりません")
	}
	now := s.now()
	// Only these fields are shared: the Project's name, the owner's note and the one video. Neither the
	// Project ID nor the path of the file is.
	return ReviewPackage{
		FileName: fmt.Sprintf("mam-review-%s-%s.zip", now.Format("20060102"), q.ID),
		manifest: domain.ReviewManifest{
			Format: domain.ReviewPackageFormat, Version: domain.ReviewVersion, ReviewID: q.ID, ProjectName: p.Name, Note: q.Note, ExportedAt: now,
			Video: domain.ReviewVideo{FileName: domain.ReviewVideoName(e.Codec), Resolution: e.Resolution, AspectRatio: e.AspectRatio, Codec: e.Codec},
		},
		videoPath: filepath.Join(s.assetsDir, filepath.FromSlash(e.FilePath)),
	}, nil
}

// Write writes the package as a ZIP of manifest.json and the video.
func (p ReviewPackage) Write(w io.Writer) error {
	zw := zip.NewWriter(w)
	if err := bundlezip.WriteJSON(zw, domain.ReviewManifestName, p.manifest.ExportedAt, p.manifest); err != nil {
		return err
	}
	f, err := os.Open(p.videoPath)
	if err != nil {
		return fmt.Errorf("open review video: %w", err)
	}
	defer func() { _ = f.Close() }()
	// The video is compressed already.
	if err := bundlezip.WriteEntry(zw, p.manifest.Video.FileName, zip.Store, p.manifest.ExportedAt, f); err != nil {
		return err
	}
	return zw.Close()
}

// decodeStrict reads one JSON value that has no fields the struct does not know: a file from someone
// else is not allowed to carry anything the structs do not name.
func decodeStrict(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

// ImportResponse imports a Review Response file (POST /projects/:id/reviews/feedback) and puts the
// result in the chat. A response that is not this format, is not for a review request of this
// Project, or is invalid is a ValidationError; one that was imported already is ErrConflict. Nothing
// is stored in either case.
func (s *ReviewService) ImportResponse(ctx context.Context, projectID string, src io.Reader) (ReviewsView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return ReviewsView{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(src, maxReviewResponseBytes+1))
	if err != nil {
		return ReviewsView{}, err
	}
	if len(raw) > maxReviewResponseBytes {
		return ReviewsView{}, invalid("response", "応答ファイルが大きすぎます（上限 1MB）")
	}
	var resp domain.ReviewResponse
	if err := decodeStrict(bytes.NewReader(raw), &resp); err != nil {
		return ReviewsView{}, invalid("response", "Nefos Video のレビュー応答（JSON）として読み取れません")
	}
	if err := resp.Validate(); err != nil {
		return ReviewsView{}, reviewInvalid("response", err)
	}
	switch err := s.reviews.ImportResponse(ctx, projectID, resp); {
	case errors.Is(err, repository.ErrNotFound):
		return ReviewsView{}, invalid("response", "このプロジェクトのレビュー依頼への応答ではありません")
	case errors.Is(err, repository.ErrConflict):
		return ReviewsView{}, conflict("この応答はすでに取り込み済みです")
	case err != nil:
		return ReviewsView{}, err
	}
	// The response is imported; a failing notice does not undo it.
	text := fmt.Sprintf("レビュー結果: %s さんが「%s」と回答しました（コメント %d 件）", resp.ReviewerName, resp.Decision.Label(), len(resp.Comments))
	if err := s.chatSvc.Announce(context.WithoutCancel(ctx), projectID, assistantText(text)); err != nil {
		s.logger.Error("post review notice failed", "project_id", projectID, "err", err)
	}
	return s.ProjectReviews(ctx, projectID, resp.ReviewID)
}

// SetCommentResolved marks an imported comment as dealt with or open again
// (PUT /projects/:id/review-comments/:commentId) and returns the review request that holds it.
func (s *ReviewService) SetCommentResolved(ctx context.Context, projectID, commentID string, resolved bool) (ReviewItemView, error) {
	it, err := s.reviews.SetCommentResolved(ctx, projectID, commentID, resolved)
	if err != nil {
		return ReviewItemView{}, notFound(err)
	}
	e, err := s.editors.GetExport(ctx, projectID, it.Request.ExportID)
	return ReviewItemView{ReviewItem: it, Export: e}, err
}
