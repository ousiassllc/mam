package editor

import (
	"context"
	"mime"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Names of the upload purpose and the download kinds of this package.
const (
	PurposeVoiceSample = "voice_sample"
	KindEditPreview    = "edit_preview"
	KindProjectExport  = "project_export"
)

func registerFiles(r *mcpkit.Registrar, svc *service.EditorService) {
	r.Files.RegisterUpload(mcpkit.UploadSpec{
		Purpose:     PurposeVoiceSample,
		Description: "話者の声のサンプル録音（音声ファイル）から声を複製し、その話者のセリフの声にする。同じ話者の新しい複製は古いものを置き換える（ElevenLabs が必要）/ Clone a speaker's voice from a sample recording.",
		MaxBytes:    service.MaxVoiceSampleBytes,
		Params: []mcpkit.UploadParam{
			{Name: "project_id", Description: "声を使う Project", Required: true},
			{Name: "speaker", Description: "台本での話者名（1〜 文字数上限まで）", Required: true},
			{Name: "name", Description: "複製した声の名前（省略すると話者名）"},
		},
		Handle: func(ctx context.Context, in mcpkit.UploadInput) (any, error) {
			p := in.Params
			clone, err := svc.CloneVoice(ctx, p["project_id"], p["speaker"], p["name"], in.Upload())
			if err != nil {
				return nil, err
			}
			return map[string]any{"voice_clone_id": clone.ID, "speaker": clone.Speaker, "name": clone.Name}, nil
		},
	})
	r.Files.RegisterDownload(KindEditPreview, func(ctx context.Context, timelineID string) (mcpkit.DownloadFile, error) {
		f, err := svc.Preview(ctx, timelineID) // renders on first use; later calls find the cached file
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		return mcpkit.DownloadFile{Name: "mam-rough-cut-" + timelineID + ".mp4", ContentType: f.MimeType, Path: f.Path}, nil
	})
	r.Files.RegisterDownload(KindProjectExport, func(ctx context.Context, id string) (mcpkit.DownloadFile, error) {
		projectID, exportID, ok := strings.Cut(id, "/")
		if !ok {
			return mcpkit.DownloadFile{}, service.ErrNotFound
		}
		f, err := svc.ExportFile(ctx, projectID, exportID)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		_, params, _ := mime.ParseMediaType(f.Disposition)
		return mcpkit.DownloadFile{Name: params["filename"], ContentType: f.MimeType, Path: f.Path}, nil
	})
}

// VoicesOutput is the cloned voices of a Project.
type VoicesOutput struct {
	Voices []VoiceOut `json:"voices"`
}

// ProjectInput names a Project.
type ProjectInput struct {
	ProjectID string `json:"project_id"`
}

func listVoices(svc *service.EditorService) mcpkit.Handler[ProjectInput, VoicesOutput] {
	return func(ctx context.Context, in ProjectInput) (VoicesOutput, error) {
		clones, err := svc.Voices(ctx, in.ProjectID)
		if err != nil {
			return VoicesOutput{}, err
		}
		out := VoicesOutput{Voices: make([]VoiceOut, 0, len(clones))}
		for _, c := range clones {
			out.Voices = append(out.Voices, VoiceOut{ID: c.ID, Speaker: c.Speaker, Name: c.Name, CreatedAt: c.CreatedAt})
		}
		return out, nil
	}
}

func previewURL(r *mcpkit.Registrar) mcpkit.Handler[TimelineInput, mcpkit.Link] {
	return func(ctx context.Context, in TimelineInput) (mcpkit.Link, error) {
		return r.Files.DownloadURL(ctx, KindEditPreview, in.TimelineID)
	}
}

// ExportInput names an export.
type ExportInput struct {
	ProjectID string `json:"project_id"`
	ExportID  string `json:"export_id" jsonschema:"get_editor_view の exports[].id"`
}

func exportURL(r *mcpkit.Registrar) mcpkit.Handler[ExportInput, mcpkit.Link] {
	return func(ctx context.Context, in ExportInput) (mcpkit.Link, error) {
		return r.Files.DownloadURL(ctx, KindProjectExport, in.ProjectID+"/"+in.ExportID)
	}
}
