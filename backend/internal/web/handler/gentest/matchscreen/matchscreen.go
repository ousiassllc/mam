// Package matchscreen holds the handlers of the prompt match screen (paths.GenerationTestMatch, #916): from one reference image the
// AI writes a prompt, a model generates with it, the AI measures how much the picture looks like the reference and fixes the prompt,
// until the target is reached. It belongs to the generation test: the pictures are results of its history, shown by its handlers. The page is
// a thin layer over service/gentest/match (a separate package of the handlers for the directory's line limit).
package matchscreen

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Handlers answer the routes of the screen.
type Handlers struct {
	Svc *gentest.Service
}

// Register adds the routes of the screen to the engine.
func Register(engine *gin.Engine, svc *gentest.Service) {
	h := Handlers{Svc: svc}
	engine.GET(paths.GenerationTestMatch(), h.Page)
	engine.POST(paths.GenerationTestPart("match", "estimate"), h.Estimate)
	engine.POST(paths.GenerationTestPart("match", "run"), h.Run)
	engine.GET(paths.GenerationTestPart("match", "batches", ":id"), h.Batch)
	engine.POST(paths.GenerationTestPart("match", "batches", ":id", "cancel"), h.Cancel)
	engine.POST(paths.GenerationTestPart("match", "batches", ":id", "adjust"), h.Adjust)
}

// matcher is the prompt match of the service; false (and a 503 answer) when the app has none.
func (h Handlers) matcher(c *gin.Context) (*match.Service, bool) {
	m := match.From(h.Svc)
	if m == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
	}
	return m, m != nil
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

// refuse is the status and message of an error of the service: 422 for an input that cannot be taken, 409 when the state does not
// allow it, 502 when the LLM failed, 404 for something that is not there; anything else is a fault (500, logged, with a generic
// message: the cause stays out of the page).
func refuse(c *gin.Context, err error) (status int, msg string) {
	var (
		invalid  *gterr.InvalidError
		conflict *gterr.ConflictError
		upstream *gterr.UpstreamError
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
	case errors.Is(err, gterr.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません。画面を読み込み直してください"
	case errors.Is(err, gterr.ErrUnavailable):
		return http.StatusServiceUnavailable, "生成テストはこのアプリでは使えません"
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "処理できませんでした。mam のログを確認してください"
}
