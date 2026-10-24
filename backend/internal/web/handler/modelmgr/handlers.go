// Package modelmgr holds the handlers of the model manager screen (paths.ModelManager, spec/api/endpoints-model-manager.md):
// the ComfyUI engine, the model catalog with the license consent, the Hugging Face token, the view of an external
// ComfyUI and the trial generations. It is a package of its own because handler is at its size limit; like handler it
// depends on the service layer and the web layer only. Every action calls modelmanager.Service (the same
// service the MCP tools call), answers with the section it changed, and turns the service's refusals into a status
// and a message with modelmanager.Refuse. view.go builds what the screen draws.
package modelmgr

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the screen.
type Handlers struct {
	Svc *modelmanager.Service
}

// Register adds the routes of the screen to the engine.
func Register(engine *gin.Engine, svc *modelmanager.Service) {
	h := Handlers{Svc: svc}
	engine.GET(paths.ModelManager(), h.Page)
	engine.GET(paths.ModelManagerPart("dialog"), h.CloseDialog)

	engine.GET(paths.ModelManagerPart("engine"), h.Engine)
	engine.GET(paths.ModelManagerPart("engine", "gpu"), h.GPU)
	engine.POST(paths.ModelManagerPart("engine", "install"), h.InstallEngine)
	engine.POST(paths.ModelManagerPart("engine", "start"), h.StartEngine)
	engine.POST(paths.ModelManagerPart("engine", "stop"), h.StopEngine)
	engine.PUT(paths.ModelManagerPart("engine", "autostart"), h.SetAutoStart)
	engine.DELETE(paths.ModelManagerPart("engine"), h.RemoveEngine)
	engine.POST(paths.ModelManagerPart("engine", "detect"), h.DetectComfyUI)
	engine.POST(paths.ModelManagerPart("jobs", ":job", "cancel"), h.CancelJob)

	engine.GET(paths.ModelManagerPart("models"), h.Models)
	engine.GET(paths.ModelManagerPart("models", ":id", "consent"), h.Consent)
	engine.POST(paths.ModelManagerPart("models", ":id", "install"), h.InstallModel)
	engine.DELETE(paths.ModelManagerPart("models", ":id"), h.DeleteModel)

	engine.PUT(paths.ModelManagerPart("hf-token"), h.SetToken)
	engine.DELETE(paths.ModelManagerPart("hf-token"), h.ClearToken)
	engine.GET(paths.ModelManagerPart("external"), h.External)

	engine.GET(paths.ModelManagerPart("trials"), h.Trials)
	engine.GET(paths.ModelManagerPart("trials", "form"), h.TrialForm)
	engine.POST(paths.ModelManagerPart("trials"), h.StartTrial)
	engine.DELETE(paths.ModelManagerPart("trials", ":id"), h.DeleteTrial)
	engine.GET(paths.ModelManagerPart("trials", ":id", "file"), h.TrialFile)
}

// Page answers GET /model-manager: the page, or the body alone for an HTMX request (a history restore wants the page).
func (h Handlers) Page(c *gin.Context) {
	data := h.data(c)
	c.Writer.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
	if c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true" {
		render(c, http.StatusOK, view.Body(data))
		return
	}
	render(c, http.StatusOK, pages.ModelManagerPage(middleware.CSRFToken(c), data))
}

// CloseDialog answers with nothing: what "閉じる" swaps into the dialog area.
func (h Handlers) CloseDialog(c *gin.Context) {
	render(c, http.StatusOK, templ.NopComponent)
}

// refuse is the status and message of an error of the service: 422 for an input that cannot be taken, 409 when
// the state does not allow it, 404 for something that is not there; anything else is a fault (500, logged, with a
// generic message: the cause stays out of the page).
func refuse(c *gin.Context, err error) (status int, msg string) {
	kind, msg := mmview.Refuse(err)
	switch kind {
	case mmview.RefusalInvalid:
		return http.StatusUnprocessableEntity, msg
	case mmview.RefusalConflict:
		return http.StatusConflict, msg
	case mmview.RefusalNotFound:
		return http.StatusNotFound, msg
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}

func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
