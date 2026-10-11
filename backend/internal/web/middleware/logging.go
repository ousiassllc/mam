package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger writes one structured line per request: 5xx as error, 4xx as warn, the rest as info. A
// handler that dropped the connection with http.ErrAbortHandler (a download that failed after it
// started) is logged as an error with the causes it attached, and the panic is passed on: net/http
// and Recovery stay quiet about it, so this line is the only trace of the failure.
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		aborted := false
		defer func() {
			if r := recover(); r != nil {
				// Any other panic is Recovery's to log.
				if r != http.ErrAbortHandler { //nolint:errorlint // net/http compares the panic value by identity
					panic(r)
				}
				aborted = true
			}
			logRequest(c, logger, start, aborted)
			if aborted {
				panic(http.ErrAbortHandler)
			}
		}()
		c.Next()
	}
}

func logRequest(c *gin.Context, logger *slog.Logger, start time.Time, aborted bool) {
	status := c.Writer.Status()
	level := slog.LevelInfo
	switch {
	case aborted || status >= http.StatusInternalServerError:
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
	if aborted {
		attrs = append(attrs, "aborted", true)
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

// Recovery turns a handler panic into a logged 500 instead of crashing the process. The
// http.ErrAbortHandler panic is passed on to net/http, which drops the connection quietly.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				// net/http's way for a handler to drop the connection (a download that failed after it
				// started): not a crash, so neither logged as one nor answered with a 500.
				if r == http.ErrAbortHandler { //nolint:errorlint // net/http compares the panic value by identity
					panic(r)
				}
				logger.Error("http handler panicked",
					"method", c.Request.Method, "path", c.Request.URL.Path,
					"panic", r, "stack", string(debug.Stack()))
				abortWith(c, http.StatusInternalServerError, "", "内部エラーが発生しました")
			}
		}()
		c.Next()
	}
}
