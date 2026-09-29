package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// isHTMX reports whether the request comes from HTMX (page routes branch on it).
func isHTMX(c *gin.Context) bool { return c.GetHeader("HX-Request") == "true" }

// statusOf maps a service error to an HTTP status and the message shown to the user.
// Unexpected errors are attached to the context (logged by the logging middleware)
// and shown as a generic message.
func statusOf(c *gin.Context, err error) (int, string) {
	var invalid *service.ValidationError
	switch {
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません"
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict, "すでに応答済みか、状態が変わっています。画面を更新してください"
	}
	_ = c.Error(err)
	return http.StatusInternalServerError, "内部エラーが発生しました"
}

// toast answers an action with an out-of-band toast and leaves the swap target untouched
// (HX-Reswap: none). The business errors of spec/api/endpoints.md §0 are reported this way.
func toast(c *gin.Context, err error) {
	status, message := statusOf(c, err)
	c.Header("HX-Reswap", "none")
	render(c, status, atoms.ToastOOB(message))
}
