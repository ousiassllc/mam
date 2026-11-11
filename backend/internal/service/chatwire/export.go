package chatwire

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
)

// The chat operations of the finished video (service/chatops/export, #1140): the render is EditorService.Export, the one the editor panel and the MCP tool export_video call;
// the links are the app's own routes (GET /projects/:id/exports/:exportId/video and /projects/:id/bundle).
func init() {
	builders = append(builders, func(env Env, reg *chatops.Runtime) {
		if env.Editor == nil {
			return
		}
		deps := exportDeps(env.Editor)
		reg.Handle(export.OpExportVideo, export.NewRender(deps))
		reg.Handle(export.OpDownloadExport, export.NewDownload(deps))
		reg.Handle(export.OpExportBundle, export.Bundle{})
	})
}

func exportDeps(ed *service.EditorService) export.Deps {
	return export.Deps{
		View: func(ctx context.Context, projectID string) (export.View, error) {
			v, err := ed.View(ctx, projectID, "")
			if err != nil {
				return export.View{}, err
			}
			timelineID := ""
			if v.Timeline != nil {
				timelineID = v.Timeline.ID
			}
			return export.View{
				State: v.Project.State, CanRender: v.CanRender, HasAudio: v.Audio != nil, AudioCurrent: v.Audio.CurrentFor(timelineID),
				Defaults: v.Defaults, Exports: v.Exports,
			}, nil
		},
		Export: func(ctx context.Context, projectID string, in export.Args) (export.Done, error) {
			v, err := ed.Export(ctx, projectID, service.ExportInput{AspectRatio: in.AspectRatio, Resolution: in.Resolution, Codec: in.Codec})
			if err != nil {
				return export.Done{}, exportRefusal(err)
			}
			done := export.Done{State: v.Project.State}
			if len(v.Exports) > 0 { // newest first: the one this run made
				e := v.Exports[0]
				done.ExportID = e.ID
				done.Options.AspectRatio, done.Options.Resolution, done.Options.Codec = e.AspectRatio, e.Resolution, e.Codec
			}
			return done, nil
		},
	}
}

// exportRefusal turns what the screen tells of a refused export into *chatops.Refusal: a state that cannot export, an audio that is not of the latest edit, another stage
// running, no ffmpeg, a failed ffmpeg run. Any other error is returned as it is.
func exportRefusal(err error) error {
	var (
		conflicted *service.ConflictError
		ai         *service.AIError
	)
	switch {
	case errors.As(err, &conflicted):
		return &chatops.Refusal{Message: conflicted.Message}
	case errors.Is(err, service.ErrStageBusy):
		return &chatops.Refusal{Message: "別の編集処理（Rough Cut・判定・音声・Final QC・書き出し）が実行中です。終わってからやり直してください"}
	case errors.Is(err, service.ErrUnavailable):
		return &chatops.Refusal{Message: "ffmpeg / ffprobe が見つからないため、書き出しは使えません。" + export.WhereToSetFFmpeg}
	case errors.Is(err, service.ErrNotFound):
		return &chatops.Refusal{Message: "書き出しに必要な編集と音声が見つかりません。音声を作ってから、もう一度お願いします"}
	case errors.As(err, &ai):
		return &chatops.Refusal{Message: ai.Reason}
	}
	return refusalOf(err)
}
