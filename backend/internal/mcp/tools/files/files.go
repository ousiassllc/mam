// Package files holds the MCP tools of file exchange: create_upload_ticket and list_upload_purposes (the
// upload side of mcp/mcpkit.Files), get_asset_download_url, and the purposes and download kinds the
// foundation provides (import_bundle, reference_image; project_asset). A slice that needs another
// purpose registers it from its own Register with r.Files.RegisterUpload; it does not add one here.
package files

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds the tools, upload purposes and download kinds of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	registerPurposes(r.Files, svc)
	r.Files.RegisterDownload(KindProjectAsset, projectAsset(svc.Projects))

	mcpkit.Read(r, "list_upload_purposes",
		"ファイルをアップロードできる用途と、それぞれの必須項目・上限サイズ / The purposes a file can be uploaded for, with their parameters and size limit.",
		func(context.Context, ListPurposesInput) (ListPurposesOutput, error) {
			return listPurposes(r.Files), nil
		})
	mcpkit.Write(r, "create_upload_ticket",
		"アップロード用のトークン付き URL を発行する。応答の method / file_field に従って、base64 ではなく multipart で 15 分以内に 1 回だけ送る / Issue a one-time, 15-minute upload URL bound to a purpose; POST the file as multipart/form-data.",
		func(ctx context.Context, in TicketInput) (mcpkit.UploadTicket, error) {
			return r.Files.UploadTicketFor(ctx, in.Purpose, in.Params)
		})
	mcpkit.Read(r, "get_asset_download_url",
		"プロジェクトにアップロードされた画像・動画（アセット）のダウンロード URL を発行する（15 分有効）/ Download URL of an uploaded Project asset (valid 15 minutes).",
		func(ctx context.Context, in AssetInput) (mcpkit.Link, error) {
			return r.Files.DownloadURL(ctx, KindProjectAsset, in.ProjectID+"/"+in.AssetID)
		})
}

// TicketInput names the purpose and the values the ticket is bound to.
type TicketInput struct {
	Purpose string            `json:"purpose" jsonschema:"list_upload_purposes が返す purpose"`
	Params  map[string]string `json:"params,omitempty" jsonschema:"用途ごとの必須項目（project_id など）。list_upload_purposes を参照"`
}

// ListPurposesInput has no field.
type ListPurposesInput struct{}

// PurposeParam is one value a purpose needs.
type PurposeParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// Purpose is one thing a file can be uploaded for.
type Purpose struct {
	Purpose     string         `json:"purpose"`
	Description string         `json:"description"`
	MaxBytes    int64          `json:"max_bytes"`
	Params      []PurposeParam `json:"params"`
}

// ListPurposesOutput is every registered purpose.
type ListPurposesOutput struct {
	Purposes []Purpose `json:"purposes"`
}

func listPurposes(f *mcpkit.Files) ListPurposesOutput {
	specs := f.UploadPurposes()
	out := ListPurposesOutput{Purposes: make([]Purpose, 0, len(specs))}
	for _, s := range specs {
		p := Purpose{Purpose: s.Purpose, Description: s.Description, MaxBytes: s.MaxBytes, Params: make([]PurposeParam, 0, len(s.Params))}
		for _, q := range s.Params {
			p.Params = append(p.Params, PurposeParam{Name: q.Name, Description: q.Description, Required: q.Required})
		}
		out.Purposes = append(out.Purposes, p)
	}
	return out
}

// AssetInput names an uploaded asset.
type AssetInput struct {
	ProjectID string `json:"project_id"`
	AssetID   string `json:"asset_id"`
}
