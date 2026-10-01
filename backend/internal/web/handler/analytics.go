package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// AnalyticsPage is the MVP Success Metrics (spec/spec.md §28) of one Project
// (GET /projects/:id/analytics) or of every Project (GET /analytics, no :id). HTMX requests get
// the metrics without the page frame.
func AnalyticsPage(svc *service.AnalyticsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := svc.Metrics(c.Request.Context(), c.Param("id"))
		if err != nil {
			pageError(c, err)
			return
		}
		data := pages.AnalyticsPageData{Project: view.Project, Metrics: view.Metrics}
		if isHTMX(c) {
			render(c, http.StatusOK, organisms.AnalyticsPanel(data.Metrics))
			return
		}
		render(c, http.StatusOK, pages.AnalyticsPage(middleware.CSRFToken(c), data))
	}
}
