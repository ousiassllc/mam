// Package matchscreen holds the handlers of the prompt match screen (paths.GenerationTestMatch, #916): from one reference image the
// AI writes a prompt, a model generates with it, the AI measures how much the picture looks like the reference and fixes the prompt,
// until the target is reached. It belongs to the generation test: the pictures are results of its history, shown by its handlers. The page is
// a thin layer over service/gentest/match (a separate package of the handlers for the directory's line limit).
package matchscreen

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
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
