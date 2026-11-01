// Package gentest holds the handlers of the generation test screen (paths.GenerationTest, spec/api/endpoints-gentest.md):
// composing a batch, confirming its estimate, running it, the history and the prompt history, the detail of a result and the
// import of a result into a Project. It belongs to no Project. The page is a thin layer over service/gentest.
package gentest

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the screen.
type Handlers struct {
	Svc *gentest.Service
}

// Register adds the routes of the screen to the engine.
func Register(engine *gin.Engine, svc *gentest.Service) {
	h := Handlers{Svc: svc}
	engine.GET(paths.GenerationTest(), h.Page)
	engine.GET(paths.GenerationTestPart("form"), h.Form)
	engine.GET(paths.GenerationTestPart("prompt-row"), h.PromptRow)
	engine.GET(paths.GenerationTestPart("picker"), h.Picker)
	engine.GET(paths.GenerationTestPart("ref-row"), h.RefRow)
	engine.POST(paths.GenerationTestPart("estimate"), h.Estimate)
	engine.POST(paths.GenerationTestPart("run"), h.Run)
	engine.GET(paths.GenerationTestPart("batches", ":id"), h.Batch)
	engine.POST(paths.GenerationTestPart("batches", ":id", "cancel"), h.Cancel)

	engine.GET(paths.GenerationTestHistory(), h.History)
	engine.GET(paths.GenerationTestPart("results", ":id"), h.Detail)
	engine.GET(paths.GenerationTestPart("results", ":id", "file"), h.ResultFile)
	engine.POST(paths.GenerationTestPart("results", ":id", "favorite"), h.Favorite)
	engine.POST(paths.GenerationTestPart("results", ":id", "import"), h.Import)
	engine.DELETE(paths.GenerationTestPart("results", ":id"), h.DeleteResult)
	engine.GET(paths.GenerationTestPart("inputs", ":id", "file"), h.InputFile)

	engine.GET(paths.GenerationTestPrompts(), h.Prompts)
	engine.DELETE(paths.GenerationTestPart("prompts", ":id"), h.DeletePrompt)
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

// htmxFragment says the request wants the part of the page, not the page (a history restore wants the page).
func htmxFragment(c *gin.Context) bool {
	c.Writer.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
	return c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true"
}

// refuse is the status and message of an error of the service: 422 for an input that cannot be taken, 409 when the state does
// not allow it (a changed estimate, a locked slot), 404 for something that is not there; anything else is a fault (500, logged,
// with a generic message: the cause stays out of the page).
func refuse(c *gin.Context, err error) (status int, msg string) {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
		upstream *gterr.UpstreamError
		partial  *projectlink.PartialImportError
		tooBig   *http.MaxBytesError
	)
	switch {
	case errors.As(err, &tooBig):
		return http.StatusRequestEntityTooLarge, "送った画像が大きすぎます"
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.As(err, &conflict):
		return http.StatusConflict, conflict.Message
	case errors.As(err, &upstream):
		return http.StatusBadGateway, upstream.Message
	case errors.As(err, &partial):
		return http.StatusConflict, "画像は取り込みましたが、スロットには割り当てられませんでした（LOCK されているか、対象が見つかりません）"
	case errors.Is(err, gterr.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません。画面を読み込み直してください"
	case errors.Is(err, gterr.ErrUnavailable):
		return http.StatusServiceUnavailable, "生成テストはこのアプリでは使えません"
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}
