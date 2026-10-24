package takes

import (
	"context"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Download kinds; the resource id is the Take ID.
const (
	KindTakeVideo     = "take_video"
	KindTakeThumbnail = "take_thumbnail"
)

func registerDownloads(f *mcpkit.Files, svc *service.TakeService) {
	f.RegisterDownload(KindTakeVideo, mediaFile(svc.Video))
	f.RegisterDownload(KindTakeThumbnail, mediaFile(svc.Thumbnail))
}

// mediaFile serves what the screen's player and thumbnail get: the same service call.
func mediaFile(get func(ctx context.Context, takeID string) (service.MediaFile, error)) mcpkit.DownloadFunc {
	return func(ctx context.Context, takeID string) (mcpkit.DownloadFile, error) {
		file, err := get(ctx, takeID)
		if err != nil {
			return mcpkit.DownloadFile{}, err
		}
		return mcpkit.DownloadFile{Name: takeID + filepath.Ext(file.Path), ContentType: file.MimeType, Path: file.Path}, nil
	}
}

// DownloadOutput is the URL of a Take's file.
type DownloadOutput struct {
	TakeID string `json:"take_id"`
	mcpkit.Link
}

func downloadURL(f *mcpkit.Files, kind string) mcpkit.Handler[TakeInput, DownloadOutput] {
	return func(ctx context.Context, in TakeInput) (DownloadOutput, error) {
		link, err := f.DownloadURL(ctx, kind, in.TakeID)
		return DownloadOutput{TakeID: in.TakeID, Link: link}, err
	}
}
