package handler

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

// render writes a Templ component as an HTML response with the given status.
func render(c *gin.Context, status int, component templ.Component) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		_ = c.Error(err)
		if !c.Writer.Written() {
			c.AbortWithStatus(http.StatusInternalServerError)
		}
	}
}
