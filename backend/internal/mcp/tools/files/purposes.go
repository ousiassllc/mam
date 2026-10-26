package files

import (
	"context"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// KindProjectAsset is the download kind of an uploaded Project asset; the resource id is "<project_id>/<asset_id>".
const KindProjectAsset = "project_asset"

func projectAsset(svc *service.ProjectService) mcpkit.DownloadFunc {
	return func(ctx context.Context, id string) (mcpkit.DownloadFile, error) {
		projectID, assetID, ok := strings.Cut(id, "/")
		if !ok {
			return mcpkit.DownloadFile{}, service.ErrNotFound
		}
		f, err := svc.Asset(ctx, projectID, assetID)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		return mcpkit.DownloadFile{Name: baseName(f.Asset.FilePath), ContentType: f.Asset.MimeType, Path: f.Path}, nil
	}
}

func baseName(p string) string { return p[strings.LastIndex(p, "/")+1:] }

// registerPurposes installs import_bundle and reference_image, which the same validation as the screens
// (service.BundleService.Import, service.ReferenceService.Upload) serves.
func registerPurposes(f *mcpkit.Files, svc *service.Services) {
	f.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     "import_bundle",
		Description: "エクスポートしたプロジェクトバンドル（ZIP）を取り込んで、新しいプロジェクトを作る / Import a Project bundle (ZIP) as a new Project.",
		MaxBytes:    service.MaxBundleBytes,
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			file, err := in.Header.Open()
			if err != nil {
				return nil, err
			}
			defer func() { _ = file.Close() }()
			p, err := svc.Bundle.Import(ctx, file, in.Header.Size)
			if err != nil {
				return nil, err
			}
			return map[string]any{"project_id": p.ID}, nil
		},
	})
	f.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     "reference_image",
		Description: "プロジェクトの Reference Library に画像を登録する / Add an image to the Reference Library of a Project.",
		MaxBytes:    service.MaxImageBytes,
		Params: []mcpkit.UploadParam{
			{Name: "project_id", Description: "登録先のプロジェクト", Required: true},
			{Name: "kind", Description: "種別: character, product, location, style, lighting", Required: true},
			{Name: "name", Description: "Library での名前", Required: true},
			{Name: "tags", Description: "タグ（カンマ区切り、省略可）"},
		},
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			p := in.Params
			view, err := svc.Reference.Upload(ctx, p["project_id"], service.UploadReferenceInput{
				File: in.Upload(), Kind: p["kind"], Name: p["name"], Tags: p["tags"],
			})
			if err != nil {
				return nil, err
			}
			for _, item := range view.Items { // newest first
				if item.Source == domain.SourceUploaded && item.Name == strings.TrimSpace(p["name"]) {
					return map[string]any{"reference_id": item.ID, "asset_id": item.AssetID}, nil
				}
			}
			return map[string]any{}, nil
		},
	})
}
