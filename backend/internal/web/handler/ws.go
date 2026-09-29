package handler

import (
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
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

		// Accept checks that Origin matches Host, so only pages served by this app can connect.
		conn, err := websocket.Accept(c.Writer, c.Request, nil)
		if err != nil {
			return // Accept has already answered the request
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := conn.CloseRead(c.Request.Context()) // cancelled when the client goes away

		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				if err := wsjson.Write(ctx, conn, e); err != nil {
					return
				}
			}
		}
	}
}
