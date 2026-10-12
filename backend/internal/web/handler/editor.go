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

const timelineReloadTrigger = "timeline-reload" // <mam-timeline-editor> reloads on it (its TIMELINE_RELOAD_EVENT)

func editorPanelData(v service.EditorView) organisms.EditorPanelData {
	d := organisms.EditorPanelData{
		Project: v.Project, BranchID: v.BranchID, PendingShots: v.PendingShots, Audio: v.Audio, TrunkAudio: v.TrunkAudio, Voices: v.Voices, QC: v.QC, Exports: v.Exports, Defaults: v.Defaults, CanRender: v.CanRender,
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
		v, err := svc.View(c.Request.Context(), c.Param("id"), c.Query("branch_id"))
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
		v, err := svc.View(c.Request.Context(), c.Param("id"), c.Query("branch_id"))
		if err != nil {
			toast(c, err)
			return
		}
		render(c, http.StatusOK, organisms.EditorActionsFragment(editorPanelData(v)))
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
		render(c, http.StatusOK, organisms.EditorActionsFragment(editorPanelData(v)))
	}
}

// roughCutForm is the body of POST /projects/:id/edit/rough-cut: the Branch to cut ("" is the trunk).
type roughCutForm struct {
	BranchID string `form:"branch_id" json:"branch_id"`
}

// RoughCut makes the Rough Cut from the Selected Takes of the trunk or of the Branch in branch_id
// (POST /projects/:id/edit/rough-cut). The timeline editor reloads on the timeline-reload event.
func RoughCut(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		var in roughCutForm
		if err := c.ShouldBind(&in); err != nil {
			return service.EditorView{}, unreadable(c, err, "branch_id", "Branch の指定を読み取れませんでした")
		}
		return svc.RoughCut(c.Request.Context(), c.Param("id"), in.BranchID)
	}, timelineReloadTrigger)
}

// JudgeTimeline runs the Editor Judge (POST /edit-timelines/:id/judge). The Judge result is drawn
// once, by the stage buttons (EditorActions), which the response replaces. The timeline editor reloads
// on the timeline-reload event: it is readonly once the Project moved on.
func JudgeTimeline(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		return svc.JudgeTimeline(c.Request.Context(), c.Param("id"))
	}, timelineReloadTrigger)
}

// RequestAdditionalShot asks the Director for the additional Shots the Editor Judge found missing
// (POST /edit-timelines/:id/request-additional-shot): the Editor → Reshoot Loop. The Project moves on to GENERATING,
// or at the limit of the Retry Policy to AWAITING_APPROVAL (Escalation card), where the timeline editor is readonly.
func RequestAdditionalShot(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		return svc.RequestAdditionalShot(c.Request.Context(), c.Param("id"))
	}, timelineReloadTrigger)
}

// GenerateAudio makes the Audio Timeline of the trunk or of the Branch in branch_id (POST
// /projects/:id/audio/generate).
func GenerateAudio(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		var in roughCutForm
		if err := c.ShouldBind(&in); err != nil {
			return service.EditorView{}, unreadable(c, err, "branch_id", "Branch の指定を読み取れませんでした")
		}
		return svc.GenerateAudio(c.Request.Context(), c.Param("id"), in.BranchID)
	}, "")
}

// CloneVoice clones the voice of a speaker from an uploaded sample (POST /projects/:id/audio/voices,
// multipart speaker / sample; optional branch_id selects the panel that is answered).
func CloneVoice(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		form, err := readMultipart(c, service.MaxVoiceSampleBytes+1<<20)
		if err != nil {
			return service.EditorView{}, err
		}
		if _, err := svc.CloneVoice(c.Request.Context(), c.Param("id"), c.PostForm("speaker"), c.PostForm("name"), optionalUpload(form, "sample")); err != nil {
			return service.EditorView{}, err
		}
		return svc.View(c.Request.Context(), c.Param("id"), c.PostForm("branch_id"))
	}, "")
}

// FinalQC runs Final QC (POST /projects/:id/final-qc). It inspects the trunk; the optional branch_id
// only keeps the editor panel of the Branch it was pressed in.
func FinalQC(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		var in roughCutForm
		if err := c.ShouldBind(&in); err != nil {
			return service.EditorView{}, unreadable(c, err, "branch_id", "Branch の指定を読み取れませんでした")
		}
		return svc.FinalQC(c.Request.Context(), c.Param("id"), in.BranchID)
	}, timelineReloadTrigger)
}

// exportForm is the body of POST /projects/:id/export. Empty fields take the defaults; branch_id
// picks the Branch to export ("" is the trunk).
type exportForm struct {
	BranchID    string `form:"branch_id" json:"branch_id"`
	AspectRatio string `form:"aspect_ratio" json:"aspect_ratio"`
	Resolution  string `form:"resolution" json:"resolution"`
	Codec       string `form:"codec" json:"codec"`
}

// Export writes the finished video (POST /projects/:id/export).
func Export(svc *service.EditorService) gin.HandlerFunc {
	return editorStage(func(c *gin.Context) (service.EditorView, error) {
		var in exportForm
		if err := c.ShouldBind(&in); err != nil {
			return service.EditorView{}, unreadable(c, err, "", "書き出しの指定を読み取れませんでした")
		}
		return svc.Export(c.Request.Context(), c.Param("id"), service.ExportInput{BranchID: in.BranchID, AspectRatio: in.AspectRatio, Resolution: in.Resolution, Codec: in.Codec})
	}, "")
}

// SaveEditTimeline saves the clips the timeline editor sends as the next version
// (PUT /edit-timelines/:id, JSON {"clips":[{shot_id,take_id,in_point,out_point,transition}]}).
// It answers 204; the editor reloads the timeline it now has.
func SaveEditTimeline(svc *service.EditorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var tl domain.EditTimeline
		if err := c.ShouldBindJSON(&tl); err != nil {
			toast(c, unreadable(c, err, "clips", "タイムラインの内容を読み取れませんでした"))
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
