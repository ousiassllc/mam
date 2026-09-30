package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// ProjectEvents streams the Project's events (WebSocket /ws/projects/:id). Only
// notifications travel over the socket ({"type","message_id"}); the client fetches the
// HTML fragments itself (spec/architecture/overview.md §7).
func ProjectEvents(svc *service.ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		events, cancel, err := svc.Subscribe(c.Request.Context(), c.Param("id"))
		if err != nil {
			status, _ := statusOf(c, err)
			c.AbortWithStatus(status)
			return
		}
		defer cancel()

		streamEvents(c, events)
	}
}
