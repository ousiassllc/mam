package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// HealthBody is the JSON of the health check on every route that serves it.
type HealthBody struct {
	OK       bool   `json:"ok"`
	Database string `json:"database"`
}

// HealthResponse is the one place a HealthStatus becomes an HTTP answer: 200 when everything the
// app needs works, 503 otherwise. GET /healthz and GET /api/v1/health both use it.
func HealthResponse(st service.HealthStatus) (int, HealthBody) {
	status := http.StatusOK
	if !st.OK {
		status = http.StatusServiceUnavailable
	}
	return status, HealthBody{OK: st.OK, Database: st.Database}
}

// Health reports readiness of the local server and its database as JSON.
func Health(svc *service.HealthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, body := HealthResponse(svc.Check(c.Request.Context()))
		c.JSON(status, body)
	}
}
