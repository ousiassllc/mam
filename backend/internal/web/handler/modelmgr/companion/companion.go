// Package companion is the handlers of the companion software of the model manager (spec/components/model-manager-screen.md, "付随ソフトの導入（#1106）"): the routes
// of the sections of ffmpeg and FaceFusion (the subpackages ffmpeg and facefusion hold their handlers) and of the list. mam fetches ffmpeg on a button (the download
// routes); it never fetches FaceFusion (spec/architecture/gentest/face-swap.md §1), so there is no route that installs it: the section only tells how and looks again.
// It is a package of its own because the directory of the handlers is full (linterly).
package companion

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/service/facefusionsettings"
	facefusionhandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/companion/facefusion"
	ffmpeghandler "github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/companion/ffmpeg"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/companion"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Services are what the page reads and writes: the paths of ffmpeg (and its download) and of FaceFusion, and the Codex engine (read for its row of the list only: it
// is installed from the sign-in of ChatGPT). A nil service is a part this build does not have: no section, no route and no row.
type Services struct {
	FFmpeg     ffmpeghandler.Services
	FaceFusion *facefusionsettings.Service
	Codex      *codex.Service
}

// Has says the build has the category at all: ffmpeg or FaceFusion is there to set up.
func (s Services) Has() bool { return s.FFmpeg.Settings != nil || s.FaceFusion != nil }

// Data is what the page draws: the list, and the section of each of ffmpeg and FaceFusion that this build has.
func Data(s Services, index view.Index) view.Data {
	d := view.Data{Index: index}
	if s.FFmpeg.Settings != nil {
		f := ffmpeghandler.SectionData(s.FFmpeg)
		d.FFmpeg = &f
	}
	if s.FaceFusion != nil {
		f := facefusionhandler.SectionData(s.FaceFusion)
		d.FaceFusion = &f
	}
	return d
}

// Register adds the routes of the sections to the engine. page answers a request that is not htmx with the page of the category; index reads the list.
func Register(engine *gin.Engine, s Services, page gin.HandlerFunc, index func(*gin.Context) view.Index) {
	if !s.Has() {
		return
	}
	engine.GET(paths.CompanionIndex(), func(c *gin.Context) {
		settingshttp.NoStore(c)
		settingshttp.Render(c, http.StatusOK, view.IndexView(index(c)))
	})
	if s.FFmpeg.Settings != nil {
		engine.GET(paths.CompanionFFmpeg(), ffmpeghandler.Show(s.FFmpeg, page))
		engine.PUT(paths.CompanionFFmpeg(), ffmpeghandler.Save(s.FFmpeg))
		if s.FFmpeg.Download != nil {
			engine.GET(paths.FFmpegDownload(), ffmpeghandler.DownloadPanel(s.FFmpeg, page))
			engine.POST(paths.FFmpegDownload(), ffmpeghandler.StartDownload(s.FFmpeg))
			engine.POST(paths.FFmpegDownloadCancel(), ffmpeghandler.CancelDownload(s.FFmpeg))
			engine.DELETE(paths.FFmpegDownload(), ffmpeghandler.RemoveDownload(s.FFmpeg))
		}
	}
	if s.FaceFusion != nil {
		engine.GET(paths.CompanionFaceFusion(), facefusionhandler.Show(s.FaceFusion, page))
		engine.PUT(paths.CompanionFaceFusion(), facefusionhandler.Save(s.FaceFusion))
	}
}
