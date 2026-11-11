// Package chatcard holds the handlers of the chat cards that take a value the chat must not keep in a message (#1140). It is a package of its own because handler is at
// its size limit; handler passes the way it reports an error.
package chatcard

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	"github.com/ousiassllc/mam/backend/internal/web/organisms"
)

// SubmitSecret stores the secret typed into the masked field of a confirmation card (POST /projects/:id/messages/:messageId/secret, field secret) and returns the answer
// that closes the card. The value is read from the form and handed to the service; it is not logged, not put in an error and not echoed. fail reports an error (a toast);
// refresh is the HX-Trigger event that makes the chat fetch the messages.
func SubmitSecret(svc *service.ChatService, fail func(*gin.Context, error), refresh string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ws, err := svc.SubmitSecret(c.Request.Context(), c.Param("id"), c.Param("messageId"), c.PostForm("secret"))
		if err != nil {
			fail(c, err)
			return
		}
		c.Header("HX-Trigger", refresh)
		settingshttp.Render(c, http.StatusOK, organisms.MessageFragment(ws.Project, ws.Messages, ws.Updated))
	}
}
