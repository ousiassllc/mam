package review

import (
	"context"
	"io"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Names of the upload purposes and the download kinds of this package.
const (
	PurposeReviewResponse      = "review_response"
	PurposeReviewPackageImport = "review_package_import"
	KindReviewPackage          = "review_package"
	KindReviewVideo            = "review_video"
	KindReviewResponse         = "review_response_file"
)

func registerFiles(r *mcpkit.Registrar, svc *service.ReviewService) {
	r.Files.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     PurposeReviewResponse,
		Description: "レビュアーから返ってきた Review Response（JSON）を、この Project のレビュー依頼への応答として取り込む。形式違い・別 Project・取り込み済みは拒否され、何も保存されない / Import a Review Response file for a Project.",
		MaxBytes:    service.MaxReviewResponseRequestBytes,
		Params:      []mcpkit.UploadParam{{Name: "project_id", Description: "レビューを依頼した Project", Required: true}},
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			f, err := in.Header.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = f.Close() }()
			v, err := svc.ImportResponse(ctx, in.Params["project_id"], f)
			if err != nil {
				return nil, err
			}
			return map[string]any{"review_id": v.Touched, "project_id": v.Project.ID}, nil
		},
	})
	r.Files.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     PurposeReviewPackageImport,
		Description: "Review Package（ZIP: manifest.json と動画 1 本）をレビュアーとして取り込み、レビューの受信箱に入れる。壊れた・余計なファイルを含む・取り込み済みは拒否される / Import a Review Package as a reviewer.",
		MaxBytes:    service.MaxReviewPackageBytes,
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			f, err := in.Header.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = f.Close() }()
			inbox, err := svc.ImportPackage(ctx, f, in.Header.Size)
			if err != nil {
				return nil, err
			}
			return map[string]any{"inbox_id": inbox.ID, "review_id": inbox.ReviewID, "project_name": inbox.ProjectName}, nil
		},
	})
	r.Files.RegisterDownload(KindReviewPackage, func(ctx context.Context, id string) (mcpkit.DownloadFile, error) {
		projectID, reviewID, err := splitID(id)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		pkg, err := svc.Package(ctx, projectID, reviewID) // checks; the archive is written while it is sent
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		return mcpkit.DownloadFile{Name: pkg.FileName, ContentType: "application/zip", Write: pkg.Write}, nil
	})
	r.Files.RegisterDownload(KindReviewVideo, func(ctx context.Context, inboxID string) (mcpkit.DownloadFile, error) {
		f, err := svc.VideoFile(ctx, inboxID)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		inbox, err := svc.Get(ctx, inboxID)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		return mcpkit.DownloadFile{Name: inbox.Video.FileName, ContentType: f.MimeType, Path: f.Path}, nil
	})
	r.Files.RegisterDownload(KindReviewResponse, func(ctx context.Context, inboxID string) (mcpkit.DownloadFile, error) {
		f, err := svc.Response(ctx, inboxID)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		return mcpkit.DownloadFile{Name: f.FileName, ContentType: "application/json", Write: func(w io.Writer) error {
			_, err := w.Write(f.Data)
			return err
		}}, nil
	})
}
