package settingspage

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

// HealthDeadline is how long the overview waits for each endpoint: an endpoint that does not answer by then counts as not
// connected (the connection test of the endpoint itself waits longer).
const HealthDeadline = 3 * time.Second

// Health answers the line of the overview card of the local endpoints (GET /settings/local-models/health): "n 件中 m 件に接続
// できます". The endpoints are tried at once, each for at most HealthDeadline. It is always 200: an endpoint list that cannot be
// read, or no endpoint, is no line, so that a card that is only a link never raises the error toast.
func Health(svc *localmodels.Service, deadline time.Duration) gin.HandlerFunc {
	if deadline <= 0 {
		deadline = HealthDeadline
	}
	return func(c *gin.Context) {
		settingshttp.NoStore(c)
		view, err := svc.View()
		var lines []molecules.StatusLine
		if err == nil && len(view.Endpoints) > 0 {
			names := make([]string, len(view.Endpoints))
			for i, e := range view.Endpoints {
				names[i] = e.Name
			}
			lines = healthLines(connected(c.Request.Context(), svc, names, deadline), len(names))
		}
		settingshttp.Render(c, http.StatusOK, molecules.ToolStatus(lines))
	}
}

// connected is how many of the endpoints answer their connection test.
func connected(ctx context.Context, svc *localmodels.Service, names []string, deadline time.Duration) int {
	var (
		wg sync.WaitGroup
		mu sync.Mutex
		ok int
	)
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, deadline)
			defer cancel()
			if res, err := svc.TestEndpoint(ctx, name); err == nil && res.OK {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return ok
}

func healthLines(ok, total int) []molecules.StatusLine {
	tone := atoms.BadgeOK
	if ok < total {
		tone = atoms.BadgeWarn
	}
	return []molecules.StatusLine{line(tone, "%d 件中 %d 件に接続できます", total, ok)}
}
