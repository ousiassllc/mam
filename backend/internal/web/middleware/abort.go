package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// abortWith ends the request with status. An HTMX request gets the out-of-band toast the handlers
// use for their own errors (HX-Reswap: none leaves the swap target alone), because htmx swaps only
// text/html error responses and anything else would leave the user looking at an unchanged screen.
// Other callers get the JSON problem {"error": problem}, or an empty body when problem is empty.
func abortWith(c *gin.Context, status int, problem, toast string) {
	if c.Writer.Written() {
		c.Abort() // the status line is gone; a second body would corrupt the response
		return
	}
	if c.GetHeader("HX-Request") == "true" {
		c.Header("HX-Reswap", "none")
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(status)
		if err := atoms.ToastOOB(toast).Render(c.Request.Context(), c.Writer); err != nil {
			_ = c.Error(err)
		}
		c.Abort()
		return
	}
	if problem == "" {
		c.AbortWithStatus(status)
		return
	}
	c.AbortWithStatusJSON(status, gin.H{"error": problem})
}
