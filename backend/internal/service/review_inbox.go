package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// The reviewer side of Team Collaboration (spec/spec.md §27.1): the inbox of imported Review Packages, their comments and the Review
// Response (moved out of review.go, which holds the owner side).

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
