// Package settingshttp is what the handlers of the settings screen share: rendering, telling an HTMX request from a
// navigation, and the headers of an answer that closes the modal (spec/components/settings-screen-routes.md §2). It is a
// package of its own because handler is at its size limit and the settings handlers live in packages beside it.
package settingshttp

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/web/molecules/modal"
)

// Render writes a component as an HTML response with status.
func Render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}

// IsHTMX reports whether a page route answers with the fragment only. A history restore (back / forward after htmx's
// cache missed) is an HX-Request too, but htmx swaps the whole <body> with the answer, so it needs the whole page. The
// answer depends on these headers, so they go into Vary.
func IsHTMX(c *gin.Context) bool {
	c.Writer.Header().Add("Vary", "HX-Request, HX-History-Restore-Request")
	return c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true"
}

// NoStore keeps an answer that holds something secret, or only true for now, out of every cache.
func NoStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
}

// CloseModal makes the answer close the modal once it is swapped in (the script listens for modal.CloseEvent). The
// answer's own body is the rows to replace out of band; what goes into the modal is empty.
func CloseModal(c *gin.Context) {
	c.Header("HX-Trigger-After-Swap", modal.CloseEvent)
}
