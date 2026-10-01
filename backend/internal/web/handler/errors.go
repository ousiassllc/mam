package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
)

// abortDownload ends a download whose body failed after the status was sent: the archive is
// incomplete, so the cause is attached to the context (logged by the logging middleware) and the
// connection is dropped (net/http's ErrAbortHandler). The client sees a failed transfer instead of
// a truncated file that looks like a finished download.
func abortDownload(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
	panic(http.ErrAbortHandler)
}

// isHTMX reports whether a page route should answer with the fragment only. A history
// restore (back/forward after htmx's cache missed) is an HX-Request too, but htmx swaps
// the whole <body> with the response, so it needs the full page. The answer depends on
// these request headers, so they are declared in Vary to keep caches from mixing the two.
func isHTMX(c *gin.Context) bool {
	h := c.Writer.Header()
	h.Add("Vary", "HX-Request, HX-History-Restore-Request")
	return c.GetHeader("HX-Request") == "true" && c.GetHeader("HX-History-Restore-Request") != "true"
}

// statusOf maps a service error to an HTTP status and the message shown to the user.
// Unexpected errors are attached to the context (logged by the logging middleware)
// and shown as a generic message.
func statusOf(c *gin.Context, err error) (int, string) {
	var invalid *service.ValidationError
	var locked *service.LockedError
	var ai *service.AIError
	var conflicted *service.ConflictError
	var brand *domain.BrandViolationError
	var request *requestError
	switch {
	case errors.As(err, &request):
		return request.status, request.message
	case errors.As(err, &brand):
		return http.StatusForbidden, "Brand / Safety Constraint に違反しています: " + brand.Message()
	case errors.Is(err, service.ErrBrandViolation):
		return http.StatusForbidden, "Brand / Safety Constraint に違反しています"
	case errors.As(err, &conflicted):
		return http.StatusConflict, conflicted.Message
	case errors.As(err, &locked) && len(locked.Parameters) > 0:
		names := make([]string, len(locked.Parameters))
		for i, p := range locked.Parameters {
			names[i] = p.Label()
		}
		return http.StatusLocked, strings.Join(names, " / ") + " は LOCK されているため変更できません。LOCK を解除してから指示してください"
	case errors.Is(err, service.ErrRateLimited):
		if errors.As(err, &ai) {
			return http.StatusTooManyRequests, ai.Reason
		}
		return http.StatusTooManyRequests, "外部の生成モデル API のレート制限に達しました。しばらく待ってから再実行してください"
	case errors.As(err, &ai):
		return http.StatusBadGateway, ai.Reason
	case errors.As(err, &invalid):
		return http.StatusUnprocessableEntity, invalid.Message
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, "対象が見つかりません"
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict, "すでに応答済みか、状態が変わっています。画面を更新してください"
	case errors.Is(err, service.ErrBusy):
		return http.StatusConflict, "この Shot はすでに生成中です"
	case errors.Is(err, service.ErrStageBusy):
		return http.StatusConflict, "別の編集処理（Rough Cut・判定・音声・Final QC・書き出し）が実行中です。終わってからやり直してください"
	case errors.Is(err, service.ErrJudgeBusy):
		return http.StatusConflict, "この Take はすでに Judge 中です"
	case errors.Is(err, service.ErrNotJudged):
		return http.StatusConflict, "この Take はまだ Judge されていません。Judge が終わってから Repair してください"
	case errors.Is(err, service.ErrAccepted):
		return http.StatusConflict, "この Take は Judge に合格しているため Repair は不要です"
	case errors.Is(err, service.ErrBudgetExceeded):
		return http.StatusPaymentRequired, "予算上限を超えるため生成できません"
	case errors.Is(err, service.ErrLocked):
		return http.StatusLocked, "LOCK されている項目は変更できません"
	case errors.Is(err, service.ErrUnavailable):
		return http.StatusServiceUnavailable, "生成機能が有効になっていません（API キーと設定を確認してください）"
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
