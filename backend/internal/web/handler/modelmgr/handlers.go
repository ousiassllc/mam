// Package modelmgr holds the handlers of the model manager screen (paths.ModelManager, spec/api/endpoints-model-manager.md):
// the ComfyUI engine, the model catalog with the license consent, the view of an external
// ComfyUI (the import from Hugging Face and its token are the child package hfimport). It is a package of its own because handler is at its size limit; like handler it
// depends on the service layer and the web layer only. Every action calls modelmanager.Service (the same
// service the MCP tools call), answers with the section it changed, and turns the service's refusals into a status
// and a message with modelmanager.Refuse. view.go builds what the screen draws.
package modelmgr

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/facemodels"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/hfimport"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/localllm"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/nodes"
	"github.com/ousiassllc/mam/backend/internal/web/handler/modelmgr/runtimes"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the screen.
type Handlers struct {
	Svc *modelmanager.Service
	// Gentest is the generation test service; its face-check models are the section "顔チェック用モデル". Nil leaves the section out.
	Gentest *gentest.Service
	// Runtimes are the local voice and lip-sync runtimes (section "ローカル音声・口パク"). Nil leaves the section and its routes out.
	Runtimes *localruntime.Service
	// Nodes are the custom nodes of the allowlist (section "追加ノード"). Nil leaves the section and its routes out.
	Nodes *modelnodes.Service
	// LLMLocal is the server the calls of the provider "local" go to (page "ローカル LLM", #1104). Nil leaves the page and its routes out.
	LLMLocal *llmlocal.Service
}

// Register adds the routes of the screen to the engine.
func Register(engine *gin.Engine, svc *modelmanager.Service, g *gentest.Service, rts *localruntime.Service, nds *modelnodes.Service, llm *llmlocal.Service) {
	h := Handlers{Svc: svc, Gentest: g, Runtimes: rts, Nodes: nds, LLMLocal: llm}
	engine.GET(paths.ModelManager(), h.Page)
	engine.GET(paths.ModelManagerPart("overview"), h.OverviewPart)
	h.registerCategories(engine)
	engine.GET(paths.ModelManagerPart("dialog"), h.CloseDialog)

	engine.GET(paths.ModelManagerPart("engine"), h.Engine)
	engine.GET(paths.ModelManagerPart("engine", "detail"), h.EngineDetail)
	engine.GET(paths.ModelManagerPart("engine", "gpu"), h.GPU)
	engine.POST(paths.ModelManagerPart("engine", "install"), h.InstallEngine)
	engine.POST(paths.ModelManagerPart("engine", "start"), h.StartEngine)
	engine.POST(paths.ModelManagerPart("engine", "stop"), h.StopEngine)
	engine.PUT(paths.ModelManagerPart("engine", "autostart"), h.SetAutoStart)
	engine.DELETE(paths.ModelManagerPart("engine"), h.RemoveEngine)
	engine.POST(paths.ModelManagerPart("engine", "detect"), h.DetectComfyUI)
	engine.POST(paths.ModelManagerPart("jobs", ":job", "cancel"), h.CancelJob)

	if rts != nil {
		runtimes.Register(engine, rts, llm)
	}
	if llm != nil {
		localllm.Register(engine, llm)
	}
	if nds != nil {
		nodes.Register(engine, nds)
	}
	if g != nil && g.FaceModels != nil {
		facemodels.Register(engine, g.FaceModels) // 顔チェック用モデル (#1026)
	}

	engine.GET(paths.ModelManagerPart("adult", "confirm"), h.AdultConfirm)
	engine.PUT(paths.ModelManagerPart("adult"), h.SetAdult)

	engine.PUT(paths.ModelManagerPart("commercial"), h.SetCommercial)

	engine.GET(paths.ModelManagerPart("models"), h.Models)
	engine.GET(paths.ModelManagerPart("models", ":id", "detail"), h.ModelDetail)
	engine.GET(paths.ModelManagerPart("models", ":id", "consent"), h.Consent)
	engine.POST(paths.ModelManagerPart("models", ":id", "install"), h.InstallModel)
	engine.DELETE(paths.ModelManagerPart("models", ":id"), h.DeleteModel)

	hfimport.Register(engine, svc)
	engine.GET(paths.ModelManagerPart("external"), h.External)
	engine.GET(paths.ModelManagerPart("external", "detail"), h.ExternalDetail)
}

// CloseDialog answers with nothing: what "閉じる" swaps into the dialog area.
func (h Handlers) CloseDialog(c *gin.Context) {
	render(c, http.StatusOK, templ.NopComponent)
}

// refuse is the status and message of an error of the service: 422 for an input that cannot be taken, 409 when
// the state does not allow it, 404 for something that is not there; anything else is a fault (500, logged, with a
// generic message: the cause stays out of the page).
func refuse(c *gin.Context, err error) (status int, msg string) {
	var invalid *gterr.InvalidError
	var conflict *gterr.ConflictError
	switch {
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.As(err, &conflict):
		return http.StatusConflict, conflict.Message
	case errors.Is(err, gterr.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません。画面を読み込み直してください"
	}
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

// faceModels is the section "顔チェック用モデル" (nil while the generation test has no face models service).
func (h Handlers) faceModels() templ.Component {
	if h.Gentest == nil {
		return nil
	}
	return facemodels.Section(h.Gentest.FaceModels)
}
