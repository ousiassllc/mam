package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
)

// registerActions registers action routes (POST/PUT/DELETE, never branch on
// HX-Request, return HTML fragments only). All of them are behind the CSRF middleware.
func registerActions(_ *gin.Engine, _ *service.Services) {}
