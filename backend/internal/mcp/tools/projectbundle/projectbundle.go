// Package projectbundle holds the MCP tool that exports a Project as a portable bundle (GET
// /projects/:id/bundle). Importing one is the upload purpose import_bundle of tools/files.
package projectbundle

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// KindProjectBundle is the download kind of a Project bundle; the resource id is the Project ID.
const KindProjectBundle = "project_bundle"

// Register adds the tool and the download kind of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	r.Files.RegisterDownload(KindProjectBundle, bundle(svc.Bundle))

	mcpkit.Read(r, "export_project_bundle",
		"プロジェクトをバンドル（ZIP: データと素材ファイル）として書き出すダウンロード URL を発行する（15 分有効）。別の mam では create_upload_ticket の purpose=import_bundle で取り込める / Download URL of the Project's portable bundle (ZIP); import it with purpose import_bundle.",
		func(ctx context.Context, in Input) (Output, error) {
			link, err := r.Files.DownloadURL(ctx, KindProjectBundle, in.ProjectID)
			return Output{Link: link}, err
		})
}

// Input names the Project.
type Input struct {
	ProjectID string `json:"project_id"`
}

// Output is the download link of the bundle.
type Output struct {
	mcpkit.Link
}

func bundle(svc *service.BundleService) mcpkit.DownloadFunc {
	return func(ctx context.Context, projectID string) (mcpkit.DownloadFile, error) {
		b, err := svc.Prepare(ctx, projectID)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		return mcpkit.DownloadFile{Name: b.FileName, ContentType: "application/zip", Write: b.Write}, nil
	}
}
