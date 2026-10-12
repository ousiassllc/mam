package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// ToastOnly makes the response of a failed HTMX request leave the page alone: HX-Reswap: none keeps
// the swap target, and HX-Push-Url: false keeps the address bar and history. main.ts swaps 4xx/5xx
// HTML so the out-of-band toast shows, and htmx then pushes hx-push-url links regardless of the swap
// style, which would leave the URL of a page that never loaded.
func ToastOnly(c *gin.Context) {
	c.Header("HX-Reswap", "none")
	c.Header("HX-Push-Url", "false")
}

// abortWith ends the request with status. An HTMX request gets the out-of-band toast the handlers
// use for their own errors (HX-Reswap: none leaves the swap target alone), because htmx swaps only
// text/html error responses and anything else would leave the user looking at an unchanged screen.
// Other callers get an RFC 7807 problem {status, title, detail} (application/problem+json, the shape
// of the Huma and NoRoute errors): title is problem, detail is the user-facing toast text, which the
// Lit components show as it is. The body is empty when problem is empty.
func abortWith(c *gin.Context, status int, problem, toast string) {
	if c.Writer.Written() {
		c.Abort() // the status line is gone; a second body would corrupt the response
		return
	}
	if c.GetHeader("HX-Request") == "true" {
		ToastOnly(c)
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
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, gin.H{"status": status, "title": problem, "detail": toast})
}
