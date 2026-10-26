package chat

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// PurposeChatAttachment is the upload purpose of a file that a chat message carries.
const PurposeChatAttachment = "chat_attachment"

func registerPurpose(f *mcpkit.Files, svc *service.Services, files *stage.Store) {
	f.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     PurposeChatAttachment,
		Description: "チャットの発言に添付する画像（jpg/png/webp/gif、20 MB まで）・動画（mp4/mov/webm、300 MB まで）。応答の upload_id を post_chat_message の attachment_upload_ids に渡す（15 分以内）/ A file for a chat message; pass the returned upload_id to post_chat_message.",
		MaxBytes:    service.MaxVideoBytes,
		Params:      []mcpkit.UploadParam{{Name: "project_id", Description: "発言するプロジェクト", Required: true}},
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			projectID := in.Params["project_id"]
			if _, err := svc.Projects.Get(ctx, projectID); err != nil {
				return nil, err
			}
			return files.Add(projectID, in)
		},
	})
}
