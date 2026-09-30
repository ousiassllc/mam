package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger writes one structured line per request: 5xx as error, 4xx as warn, the rest as info.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		level := slog.LevelInfo
		switch {
		case status >= http.StatusInternalServerError:
			level = slog.LevelError
		case status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"duration", time.Since(start),
			"hx_request", c.GetHeader("HX-Request") == "true",
		}
		// Handlers attach unexpected errors with c.Error; without this the cause of a 500 is lost.
		if len(c.Errors) > 0 {
			errs := make([]error, len(c.Errors))
			for i, e := range c.Errors {
				errs[i] = e.Err
			}
			attrs = append(attrs, "err", errors.Join(errs...))
		}
		logger.Log(c.Request.Context(), level, "http request", attrs...)
	}
}

// Recovery turns a handler panic into a logged 500 instead of crashing the process.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("http handler panicked",
					"method", c.Request.Method, "path", c.Request.URL.Path,
					"panic", r, "stack", string(debug.Stack()))
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
