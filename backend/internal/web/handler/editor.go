package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

func editorPanelData(v service.EditorView) organisms.EditorPanelData {
	d := organisms.EditorPanelData{
		Project: v.Project, Audio: v.Audio, QC: v.QC, Exports: v.Exports, Defaults: v.Defaults, CanRender: v.CanRender,
	}
	if t := v.Timeline; t != nil {
		d.TimelineID, d.Clips, d.Duration, d.Judge = t.ID, len(t.Clips), t.Duration, t.Judge
	}
	return d
}

// EditorPage renders the editor of a Project (GET /projects/:id/edit): the full page, or the
// workspace for HTMX. The timeline itself is loaded by the component from the JSON API.
func EditorPage(svc *service.EditorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, err := svc.View(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		d := editorPanelData(v)
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.EditorWorkspace(d))
			return
		}
		render(c, http.StatusOK, pages.EditorPage(middleware.CSRFToken(c), d))
	}
}

// EditorActions renders the stage buttons of a Project (GET /projects/:id/edit/actions): what
// #editor-actions reloads from once the timeline editor saved.
func EditorActions(svc *service.EditorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, err := svc.View(c.Request.Context(), c.Param("id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.EditorActions(editorPanelData(v)))
	}
}

// editorStage answers a stage action with the refreshed stage buttons. Errors are toasts.
func editorStage(run func(c *gin.Context) (service.EditorView, error), trigger string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, err := run(c)
		if err != nil {
			toast(c, err)
			return
		}
		if trigger != "" {
			c.Header("HX-Trigger", trigger)
		}
		render(c, http.StatusOK, organisms.EditorActions(editorPanelData(v)))
	}
}

// RoughCut makes the Rough Cut from the Selected Takes (POST /projects/:id/edit/rough-cut). The
// timeline editor reloads on the timeline-reload event the response carries.
func RoughCut(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		return svc.RoughCut(c.Request.Context(), c.Param("id"))
	}, "timeline-reload")
}

// JudgeTimeline runs the Editor Judge (POST /edit-timelines/:id/judge).
func JudgeTimeline(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		return svc.JudgeTimeline(c.Request.Context(), c.Param("id"))
	}, "")
}

// GenerateAudio makes the simple Audio Timeline (POST /projects/:id/audio/generate).
func GenerateAudio(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		return svc.GenerateAudio(c.Request.Context(), c.Param("id"))
	}, "")
}

// FinalQC runs Final QC (POST /projects/:id/final-qc).
func FinalQC(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		return svc.FinalQC(c.Request.Context(), c.Param("id"))
	}, "timeline-reload")
}

// exportForm is the body of POST /projects/:id/export. Empty fields take the defaults.
type exportForm struct {
	AspectRatio string `form:"aspect_ratio" json:"aspect_ratio"`
	Resolution  string `form:"resolution" json:"resolution"`
	Codec       string `form:"codec" json:"codec"`
}

// Export writes the finished video (POST /projects/:id/export).
func Export(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		var in exportForm
		if err := c.ShouldBind(&in); err != nil {
			return service.EditorView{}, &service.ValidationError{Message: "書き出しの指定を読み取れませんでした"}
		}
		return svc.Export(c.Request.Context(), c.Param("id"), service.ExportInput{AspectRatio: in.AspectRatio, Resolution: in.Resolution, Codec: in.Codec})
	}, "")
}

// SaveEditTimeline saves the clips the timeline editor sends as the next version
// (PUT /edit-timelines/:id, JSON {"clips":[{shot_id,take_id,in_point,out_point,transition}]}).
// It answers 204; the editor reloads the timeline it now has.
func SaveEditTimeline(svc *service.EditorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var tl domain.EditTimeline
		if err := c.ShouldBindJSON(&tl); err != nil {
			toast(c, &service.ValidationError{Field: "clips", Message: "タイムラインの内容を読み取れませんでした"})
			return
		}
		if _, err := svc.SaveTimeline(c.Request.Context(), c.Param("id"), tl); err != nil {
			toast(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// EditPreview serves the rendered Rough Cut (GET /edit-timelines/:id/preview).
func EditPreview(svc *service.EditorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := svc.Preview(c.Request.Context(), c.Param("id"))
		serveMedia(c, file, err)
	}
}

// ExportVideo serves an exported video as a download (GET /projects/:id/exports/:exportId/video).
func ExportVideo(svc *service.EditorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := svc.ExportFile(c.Request.Context(), c.Param("id"), c.Param("exportId"))
		serveMedia(c, file, err)
	}
}
