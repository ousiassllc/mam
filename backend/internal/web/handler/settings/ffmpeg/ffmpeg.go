// Package ffmpeg is the handlers of the "ffmpeg" category of the settings screen (spec/components/settings-screen-categories.md
// §6): the page body and the save of the paths of ffmpeg and ffprobe (PUT /settings/ffmpeg).
package ffmpeg

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/ffmpeg"
)

const (
	unreadable    = "設定ファイル（ffmpeg.json）を読めません。壊れていれば、直すか削除してください"
	writeFailed   = "パスを保存できませんでした。データフォルダの権限と空き容量を確認してください"
	savedNotice   = "保存しました。次に mam を起動したときから使われます"
	clearedNotice = "パスの指定を解除しました。次に mam を起動したときから、自動で探します"
)

// SectionData is the saved paths and what the lookup finds with them. A settings file that cannot be read is the Problem of
// the category, so that the rest of the screen still works.
func SectionData(svc *ffmpegsettings.Service) view.Data {
	v, err := svc.View()
	if err != nil {
		return view.Data{Problem: unreadable}
	}
	return view.Data{FFmpeg: tool(v.FFmpeg, view.FieldFFmpeg), FFprobe: tool(v.FFprobe, view.FieldFFprobe), SearchDirs: v.SearchDirs}
}

func tool(t ffmpegsettings.Tool, field string) view.Tool {
	out := view.Tool{Label: t.Name, Field: field, Configured: t.Configured, Found: t.Path}
	var pathErr *locate.PathError
	switch {
	case t.Err == nil:
	case errors.As(t.Err, &pathErr):
		out.Problem = "指定したパスは使えません: " + pathReason(pathErr)
	default:
		out.Problem = "探した場所のどこにもありません"
	}
	return out
}

// Save sets the paths of the form (PUT /settings/ffmpeg, fields ffmpeg and ffprobe; an empty one clears the path) and answers
// with the category: 200 with the notice, or 422 with the reason and what was typed when a path cannot be used (nothing is
// then written).
func Save(svc *ffmpegsettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := locate.Config{FFmpeg: c.PostForm(view.FieldFFmpeg), FFprobe: c.PostForm(view.FieldFFprobe)}
		err := svc.Set(cfg)
		d := SectionData(svc)
		status := http.StatusOK
		var pathErr *locate.PathError
		switch {
		case err == nil && cfg.FFmpeg == "" && cfg.FFprobe == "":
			d.Notice = clearedNotice
		case err == nil:
			d.Notice = savedNotice
		case errors.As(err, &pathErr):
			status = http.StatusUnprocessableEntity
			d.Error = pathErr.Name + " のパス（" + pathErr.Path + "）を保存できませんでした。" + pathReason(pathErr)
			d.FFmpeg.Configured, d.FFprobe.Configured = cfg.FFmpeg, cfg.FFprobe
		default:
			_ = c.Error(err)
			status, d.Error = http.StatusInternalServerError, writeFailed
		}
		settingshttp.Render(c, status, view.Section(d))
	}
}

// pathReason is why a path was refused, for the person who typed it.
func pathReason(e *locate.PathError) string {
	if errors.Is(e, locate.ErrNotAbsolute) {
		return "絶対パスではありません。/ や C:\\ から始まるパスを指定してください"
	}
	return "実在する実行ファイルではありません。ファイルの場所と、実行できる権限を確認してください"
}
