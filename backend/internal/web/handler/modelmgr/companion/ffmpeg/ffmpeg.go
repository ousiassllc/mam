// Package ffmpeg is the handlers of the section "ffmpeg" of the companion software of the model manager (spec/api/endpoints-model-manager.md §1; it was the
// category "ffmpeg" of the settings screen, #1106): the section and the save of the paths of ffmpeg and ffprobe (PUT /model-manager/companion/ffmpeg).
package ffmpeg

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/ffmpegsettings"
	"github.com/ousiassllc/mam/backend/internal/web/handler/failure"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion/ffmpeg"
)

const (
	unreadable    = "設定ファイル（ffmpeg.json）を読めません。壊れていれば、直すか削除してください"
	savedNotice   = "保存しました。すぐに使われます（mam を再起動する必要はありません）"
	clearedNotice = "パスの指定を解除しました。すぐに、自動で探した ffmpeg に切り替わります"
)

// SectionData is the saved paths and what the lookup finds with them. A settings file that cannot be read is the Problem of
// the section, so that the rest of the screen still works.
func SectionData(s Services) view.Data {
	v, err := s.Settings.View()
	if err != nil {
		return view.Data{Problem: unreadable}
	}
	d := view.Data{FFmpeg: tool(v.FFmpeg, view.FieldFFmpeg), FFprobe: tool(v.FFprobe, view.FieldFFprobe), SearchDirs: v.SearchDirs}
	if s.Download != nil {
		dl := DownloadData(s)
		d.Download = &dl
	}
	return d
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

// Show answers with the section as it is now (GET /model-manager/companion/ffmpeg): the lookup runs again. A request that is not htmx gets the page.
func Show(s Services, page gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !settingshttp.IsHTMX(c) {
			page(c)
			return
		}
		settingshttp.NoStore(c)
		settingshttp.Render(c, http.StatusOK, view.Section(SectionData(s)))
	}
}

// Save sets the paths of the form (PUT /model-manager/companion/ffmpeg, fields ffmpeg and ffprobe; an empty one clears the path) and answers
// with the section: 200 with the notice, or 422 with the reason and what was typed when a path cannot be used (nothing is
// then written).
func Save(s Services) gin.HandlerFunc {
	svc := s.Settings
	return func(c *gin.Context) {
		cfg := locate.Config{FFmpeg: c.PostForm(view.FieldFFmpeg), FFprobe: c.PostForm(view.FieldFFprobe)}
		err := svc.Set(cfg)
		d := SectionData(s)
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
			status, d.Error = failure.Save(err)
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
