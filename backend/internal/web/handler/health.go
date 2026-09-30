package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// Health reports readiness of the local server and its database as JSON.
func Health(svc *service.HealthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		st := svc.Check(c.Request.Context())
		status := http.StatusOK
		if !st.OK {
			status = http.StatusServiceUnavailable
		}
		c.JSON(status, gin.H{"ok": st.OK, "database": st.Database})
	}
}
