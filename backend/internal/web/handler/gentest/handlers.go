// Package gentest holds the handlers of the generation test screen (paths.GenerationTest, spec/api/endpoints-gentest.md):
// composing a batch, confirming its estimate, running it, the history and the prompt history, the detail of a result and the
// import of a result into a Project. It belongs to no Project. The page is a thin layer over service/gentest.
package gentest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/exportscreen"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/faceswapscreen"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gtshare"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/inpaintscreen"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/matchscreen"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/posescreen"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/recipescreen"
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
	engine.GET(paths.GenerationTestPart("models"), h.Models)
	engine.GET(paths.GenerationTestPart("easy-pick"), h.EasyPick)
	engine.GET(paths.GenerationTestPart("prompt-row"), h.PromptRow)
	engine.GET(paths.GenerationTestPart("picker"), h.Picker)
	engine.GET(paths.GenerationTestPart("ref-row"), h.RefRow)
	engine.POST(paths.GenerationTestPart("input-check"), h.InputCheck)
	engine.POST(paths.GenerationTestPart("estimate"), h.Estimate)
	engine.POST(paths.GenerationTestPart("run"), h.Run)
	engine.POST(paths.GenerationTestPart("run-notice"), h.RunNotice)
	engine.GET(paths.GenerationTestPart("batches", ":id"), h.Batch)
	engine.POST(paths.GenerationTestPart("batches", ":id", "cancel"), h.Cancel)

	engine.GET(paths.GenerationTestHistory(), h.History)
	engine.GET(paths.GenerationTestPart("results", ":id"), h.Detail)
	engine.GET(paths.GenerationTestPart("results", ":id", "file"), h.ResultFile)
	engine.POST(paths.GenerationTestPart("results", ":id", "favorite"), h.Favorite)
	engine.POST(paths.GenerationTestPart("results", ":id", "import"), h.Import)
	engine.POST(paths.GenerationTestPart("results", ":id", "upscale"), h.Upscale)
	engine.DELETE(paths.GenerationTestPart("results", ":id"), h.DeleteResult)
	engine.GET(paths.GenerationTestPart("inputs", ":id", "file"), h.InputFile)

	recipescreen.Register(engine, svc, RequestOfForm(svc)) // #977
	posescreen.Register(engine, svc)
	matchscreen.Register(engine, svc)
	inpaintscreen.Register(engine, svc)
	exportscreen.Register(engine, svc)
	faceswapscreen.Register(engine, svc) // #995

	engine.GET(paths.GenerationTestPrompts(), h.Prompts)
	engine.DELETE(paths.GenerationTestPart("prompts", ":id"), h.DeletePrompt)
}

// htmxFragment says the request wants the part of the page, not the page (a history restore wants the page).
func htmxFragment(c *gin.Context) bool {
	c.Writer.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
	return c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true"
}

// refuse is the status and message of an error of the service (gtshare.Refuse), and the one only this screen has: an import that
// put the image into the Project but not into the slot (409).
func refuse(c *gin.Context, err error) (status int, msg string) {
	var partial *projectlink.PartialImportError
	if errors.As(err, &partial) {
		return http.StatusConflict, "画像は取り込みましたが、スロットには割り当てられませんでした（LOCK されているか、対象が見つかりません）"
	}
	return gtshare.Refuse(c, err)
}
