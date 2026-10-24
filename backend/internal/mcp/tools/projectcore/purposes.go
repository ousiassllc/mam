package projectcore

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func registerPurpose(f *mcpkit.Files, files *stage.Store) {
	f.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     PurposeNewProjectAttachment,
		Description: "新しいプロジェクトの最初の発言に添付する画像（jpg/png/webp/gif、20 MB まで）・動画（mp4/mov/webm、300 MB まで）。応答の upload_id を create_project の attachment_upload_ids に渡す（15 分以内）/ A file for the first message of a new Project; pass the returned upload_id to create_project.",
		MaxBytes:    service.MaxVideoBytes,
		Handle: func(_ context.Context, in mcpkit.UploadInput) (any, error) {
			return files.Add("", in)
		},
	})
}
