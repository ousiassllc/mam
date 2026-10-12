// Package health answers the health check of the local server. It is split from handler (a Templ /
// HTMX package) because it only reports the readiness JSON that GET /healthz and GET /api/v1/health share.
package health

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// Body is the JSON of the health check on every route that serves it.
type Body struct {
	OK       bool   `json:"ok"`
	Database string `json:"database"`
}

// Response is the one place a HealthStatus becomes an HTTP answer: 200 when everything the
// app needs works, 503 otherwise. GET /healthz and GET /api/v1/health both use it.
func Response(st service.HealthStatus) (int, Body) {
	status := http.StatusOK
	if !st.OK {
		status = http.StatusServiceUnavailable
	}
	return status, Body{OK: st.OK, Database: st.Database}
}

// Handler reports readiness of the local server and its database as JSON.
func Handler(svc *service.HealthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, body := Response(svc.Check(c.Request.Context()))
		c.JSON(status, body)
	}
}
