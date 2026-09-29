// Package handler holds the Gin handlers. Handlers depend on service and domain
// only; they never touch repository or agent directly.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/web/middleware"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// Home renders the landing page (page route: returns a full page).
func Home(c *gin.Context) {
	render(c, http.StatusOK, pages.HomePage(middleware.CSRFToken(c)))
}
