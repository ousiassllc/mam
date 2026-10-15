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
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/review"
)

// Team Collaboration limits (spec/spec.md §27.1). The files come from other people, so what is read
// from them is bounded.
const (
	maxReviewVideoBytes    = 2 << 30 // the video of a package
	MaxReviewPackageBytes  = maxReviewVideoBytes + 1<<20
	maxReviewResponseBytes = 1 << 20 // a Review Response and manifest.json
	// MaxReviewResponseRequestBytes caps the upload of a Review Response: the file plus the form overhead.
	MaxReviewResponseRequestBytes = maxReviewResponseBytes + 1<<20
	reviewVideoSniffBytes         = 16
	reviewsDir                    = domain.ReviewInboxDir + "/" // below the assets directory: the videos of imported packages
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
	// Exports are the Exports a review can be requested for, newest first.
	Exports []domain.Export
}

// ProjectReviews returns the review screen of a Project.
func (s *ReviewService) ProjectReviews(ctx context.Context, projectID string) (ReviewsView, error) {
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
	view := ReviewsView{Project: p, Exports: exports}
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
	return s.ProjectReviews(ctx, projectID)
}

func (s *ReviewService) exportFileExists(e domain.Export) bool {
	if !domain.ValidBundleFilePath(e.ProjectID, e.FilePath) {
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
		return ReviewsView{}, invalid("response", "MAM のレビュー応答（JSON）として読み取れません")
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
	return s.ProjectReviews(ctx, projectID)
}

// SetCommentResolved marks an imported comment as dealt with or open again
// (PUT /projects/:id/review-comments/:commentId).
func (s *ReviewService) SetCommentResolved(ctx context.Context, projectID, commentID string, resolved bool) (ReviewsView, error) {
	if _, err := s.getProject(ctx, projectID); err != nil {
		return ReviewsView{}, err
	}
	if err := s.reviews.SetCommentResolved(ctx, projectID, commentID, resolved); err != nil {
		return ReviewsView{}, notFound(err)
	}
	return s.ProjectReviews(ctx, projectID)
}

// Inbox lists the packages this instance imported as a reviewer, newest first.
func (s *ReviewService) Inbox(ctx context.Context) ([]domain.ReviewInbox, error) {
	return s.reviews.ListInbox(ctx)
}

// Get returns an imported package with the reviewer's comments.
func (s *ReviewService) Get(ctx context.Context, id string) (domain.ReviewInbox, error) {
	i, err := s.reviews.GetInbox(ctx, id)
	return i, notFound(err)
}

// ImportPackage imports a Review Package as a reviewer (POST /reviews/import). The archive holds
// exactly manifest.json and one video whose name and container agree with the manifest; anything
// else, a broken manifest, an oversized entry or a package imported before is refused (a
// ValidationError, ErrConflict for the last) and nothing is stored.
func (s *ReviewService) ImportPackage(ctx context.Context, src io.ReaderAt, size int64) (domain.ReviewInbox, error) {
	if s.assetsDir == "" {
		return domain.ReviewInbox{}, errors.New("review import is not configured: assets directory is empty")
	}
	if size <= 0 || size > MaxReviewPackageBytes {
		return domain.ReviewInbox{}, invalid("package", "レビューパッケージのサイズが正しくありません（上限 2GB）")
	}
	zr, err := zip.NewReader(src, size)
	if err != nil {
		return domain.ReviewInbox{}, invalid("package", "レビューパッケージ（ZIP）として読み取れません")
	}
	manifest, video, err := readReviewPackage(zr)
	if err != nil {
		return domain.ReviewInbox{}, err
	}
	id := domain.NewID()
	inbox := domain.ReviewInbox{
		ID: id, ReviewID: manifest.ReviewID, ProjectName: manifest.ProjectName, Note: manifest.Note,
		VideoPath: reviewsDir + id + "/" + manifest.Video.FileName, Video: manifest.Video, ResponseID: domain.NewID(),
	}
	abs := filepath.Join(s.assetsDir, filepath.FromSlash(inbox.VideoPath))
	if err := os.MkdirAll(filepath.Dir(abs), assetDirPerm); err != nil {
		return domain.ReviewInbox{}, fmt.Errorf("create review directory: %w", err)
	}
	err = fsutil.WriteAtomic(abs, assetFilePerm, func(w io.Writer) error {
		rc, err := video.Open()
		if err != nil {
			return invalid("package", "動画を開けません")
		}
		defer func() { _ = rc.Close() }()
		n, err := io.Copy(w, io.LimitReader(rc, maxReviewVideoBytes+1))
		if err == nil && n > maxReviewVideoBytes {
			err = invalid("package", "動画が大きすぎます（上限 2GB）")
		}
		return err
	})
	if err == nil {
		err = s.reviews.CreateInbox(ctx, inbox)
	}
	if err != nil {
		_ = os.RemoveAll(filepath.Dir(abs))
		if errors.Is(err, repository.ErrConflict) {
			return domain.ReviewInbox{}, conflict("このレビューパッケージはすでに読み込み済みです")
		}
		return domain.ReviewInbox{}, err
	}
	return s.Get(ctx, id)
}

// readReviewPackage checks the entries of the archive against the allowlist (manifest.json and the
// one video the manifest names) and returns the manifest and the video entry.
func readReviewPackage(zr *zip.Reader) (domain.ReviewManifest, *zip.File, error) {
	var (
		manifestFile, video *zip.File
		manifest            domain.ReviewManifest
	)
	if len(zr.File) != 2 {
		return manifest, nil, invalid("package", "レビューパッケージの中身が正しくありません（manifest.json と動画 1 本だけが入ります）")
	}
	for _, f := range zr.File {
		switch f.Name {
		case domain.ReviewManifestName:
			manifestFile = f
		case "video.mp4", "video.webm":
			video = f
		}
	}
	if manifestFile == nil || video == nil {
		return manifest, nil, invalid("package", "レビューパッケージの中身が正しくありません（manifest.json と動画 1 本だけが入ります）")
	}
	if manifestFile.UncompressedSize64 > maxReviewResponseBytes || video.UncompressedSize64 > maxReviewVideoBytes {
		return manifest, nil, invalid("package", "レビューパッケージのファイルが大きすぎます")
	}
	rc, err := manifestFile.Open()
	if err != nil {
		return manifest, nil, invalid("package", "manifest.json を開けません")
	}
	defer func() { _ = rc.Close() }()
	if err := decodeStrict(io.LimitReader(rc, maxReviewResponseBytes+1), &manifest); err != nil {
		return manifest, nil, invalid("package", "manifest.json を読み取れません")
	}
	if err := manifest.Validate(); err != nil {
		return manifest, nil, reviewInvalid("package", err)
	}
	if manifest.Video.FileName != video.Name {
		return manifest, nil, invalid("package", "manifest.json の video.file_name と動画のエントリが一致しません")
	}
	vr, err := video.Open()
	if err != nil {
		return manifest, nil, invalid("package", "動画を開けません")
	}
	defer func() { _ = vr.Close() }()
	head := make([]byte, reviewVideoSniffBytes)
	n, _ := io.ReadFull(vr, head)
	if !domain.HasReviewVideoSignature(video.Name, head[:n]) {
		return manifest, nil, invalid("package", "動画のファイル形式が %s と一致しません", video.Name)
	}
	return manifest, video, nil
}

// VideoFile returns the video of an imported package (GET /reviews/:id/video).
func (s *ReviewService) VideoFile(ctx context.Context, id string) (MediaFile, error) {
	i, err := s.Get(ctx, id)
	if err != nil {
		return MediaFile{}, err
	}
	path := filepath.Join(s.assetsDir, filepath.FromSlash(i.VideoPath))
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return MediaFile{}, ErrNotFound
	}
	return MediaFile{Path: path, MimeType: domain.ReviewVideoMime(i.Video.FileName)}, nil
}

// AddComment adds a comment of the reviewer (POST /reviews/:id/comments). timecode is the position
// as typed (empty: the video as a whole).
func (s *ReviewService) AddComment(ctx context.Context, id, timecode, body string) (domain.ReviewInbox, error) {
	tc, err := domain.ParseReviewTimecode(timecode)
	if err != nil {
		return domain.ReviewInbox{}, reviewInvalid("timecode", err)
	}
	c := domain.ReviewCommentInput{TimecodeMs: tc, Body: strings.TrimSpace(body)}
	if err := c.Validate(); err != nil {
		return domain.ReviewInbox{}, reviewInvalid("body", err)
	}
	i, err := s.reviews.AddInboxComment(ctx, id, c)
	switch {
	case errors.Is(err, repository.ErrConflict):
		return i, invalid("body", "コメントは %d 件までです", domain.MaxReviewComments)
	case err != nil:
		return i, notFound(err)
	}
	return i, nil
}

// DeleteComment removes a comment of the reviewer (DELETE /reviews/:id/comments/:commentId).
func (s *ReviewService) DeleteComment(ctx context.Context, id, commentID string) (domain.ReviewInbox, error) {
	i, err := s.reviews.DeleteInboxComment(ctx, id, commentID)
	return i, notFound(err)
}

// Decide writes the reviewer's name, decision and overall comment (PUT /reviews/:id/decision).
func (s *ReviewService) Decide(ctx context.Context, id, reviewerName, decision, comment string) (domain.ReviewInbox, error) {
	name := strings.TrimSpace(reviewerName)
	if err := domain.ValidateReviewerName(name); err != nil {
		return domain.ReviewInbox{}, reviewInvalid("reviewer_name", err)
	}
	d := domain.ReviewDecision(decision)
	if !d.Valid() {
		return domain.ReviewInbox{}, invalid("decision", "判定を選んでください（承認 / 修正依頼）")
	}
	comment = strings.TrimSpace(comment)
	if len([]rune(comment)) > domain.MaxReviewCommentRunes {
		return domain.ReviewInbox{}, invalid("comment", "全体コメントは %d 文字以内で入力してください", domain.MaxReviewCommentRunes)
	}
	i, err := s.reviews.SetDecision(ctx, id, name, d, comment)
	return i, notFound(err)
}

// ReviewResponseFile is a Review Response ready to be served.
type ReviewResponseFile struct {
	FileName string
	Data     []byte
}

// Response builds the Review Response file of an imported package (GET /reviews/:id/response).
// ErrConflict until a decision has been written.
func (s *ReviewService) Response(ctx context.Context, id string) (ReviewResponseFile, error) {
	i, err := s.Get(ctx, id)
	if err != nil {
		return ReviewResponseFile{}, err
	}
	resp, ok := i.Response(s.now())
	if !ok {
		return ReviewResponseFile{}, conflict("判定を書いてから応答を書き出してください")
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return ReviewResponseFile{}, err
	}
	return ReviewResponseFile{FileName: "mam-review-response-" + i.ResponseID + ".json", Data: data}, nil
}
