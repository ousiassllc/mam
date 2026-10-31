package mcpkit

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Error codes of a failed tool call. The text of the error is "<code>: <message>", so that an agent can
// branch on the code and a person can read the message.
const (
	CodeValidation  = "validation"      // the input is wrong; the message names the field
	CodeNotFound    = "not_found"       // the addressed object does not exist
	CodeConflict    = "conflict"        // the object is not in the state the operation needs
	CodeLocked      = "locked"          // the change would touch a LOCKed field
	CodeForbidden   = "forbidden"       // a LOCK Brand / Safety Constraint forbids it
	CodeBudget      = "budget_exceeded" // not even one Take fits the budget ceiling
	CodeRateLimited = "rate_limited"    // an external model API answered 429
	CodeUnavailable = "unavailable"     // the feature is not configured (no model, no job queue, ...)
	CodeUpstream    = "upstream"        // the Reasoning LLM could not give a usable answer
	CodeInternal    = "internal"        // anything else; the cause is in the mam log, not in the message
)

// errorCodes lists every code, to read the code back from the text of an error.
var errorCodes = []string{
	CodeValidation, CodeNotFound, CodeConflict, CodeLocked, CodeForbidden, CodeBudget, CodeRateLimited,
	CodeUnavailable, CodeUpstream, CodeInternal,
}

// ToolError is the error of a tool call as the agent sees it.
type ToolError struct {
	Code    string
	Message string
	// Status is the HTTP status the file endpoint answers with for the same error.
	Status int
}

func (e *ToolError) Error() string { return e.Code + ": " + e.Message }

// MapError turns an error of the service layer into a *ToolError, with the messages and statuses the
// screens use (web/handler.statusOf), so that a tool and a screen refuse the same request the same way.
// An error it does not know becomes CodeInternal without its text, which can carry a path or a vendor
// response; Internal tells the caller to log the cause.
func MapError(err error) *ToolError {
	var (
		te         *ToolError
		invalid    *service.ValidationError
		locked     *service.LockedError
		ai         *service.AIError
		conflicted *service.ConflictError
		brand      *domain.BrandViolationError
	)
	switch {
	case errors.As(err, &te):
		return te
	case errors.As(err, &brand):
		return &ToolError{CodeForbidden, "Brand / Safety Constraint に違反しています: " + brand.Message(), http.StatusForbidden}
	case errors.Is(err, service.ErrBrandViolation):
		return &ToolError{CodeForbidden, "Brand / Safety Constraint に違反しています", http.StatusForbidden}
	case errors.As(err, &conflicted):
		return &ToolError{CodeConflict, conflicted.Message, http.StatusConflict}
	case errors.As(err, &locked) && len(locked.Parameters) > 0:
		names := make([]string, len(locked.Parameters))
		for i, p := range locked.Parameters {
			names[i] = p.Label()
		}
		return &ToolError{CodeLocked, strings.Join(names, " / ") + " は LOCK されているため変更できません。LOCK を解除してから指示してください", http.StatusLocked}
	case errors.Is(err, service.ErrRateLimited):
		if errors.As(err, &ai) {
			return &ToolError{CodeRateLimited, ai.Reason, http.StatusTooManyRequests}
		}
		return &ToolError{CodeRateLimited, "外部の生成モデル API のレート制限に達しました。しばらく待ってから再実行してください", http.StatusTooManyRequests}
	case errors.As(err, &ai):
		return &ToolError{CodeUpstream, ai.Reason, http.StatusBadGateway}
	case errors.As(err, &invalid):
		return &ToolError{CodeValidation, invalid.Error(), http.StatusUnprocessableEntity}
	}
	for _, m := range sentinels {
		if errors.Is(err, m.err) {
			return &ToolError{m.code, m.message, m.status}
		}
	}
	return &ToolError{CodeInternal, "予期しないエラーです。mam のログを確認してください", http.StatusInternalServerError}
}

// Internal reports whether MapError does not know err, that is, whether the caller should log it.
func Internal(err error) bool { return MapError(err).Code == CodeInternal }

var sentinels = []struct {
	err     error
	code    string
	message string
	status  int
}{
	{service.ErrNotFound, CodeNotFound, "対象が見つかりません", http.StatusNotFound},
	{service.ErrConflict, CodeConflict, "すでに応答済みか、状態が変わっています", http.StatusConflict},
	{service.ErrBusy, CodeConflict, "この Shot はすでに生成中です", http.StatusConflict},
	{service.ErrStageBusy, CodeConflict, "別の編集処理（Rough Cut・判定・音声・Final QC・書き出し）が実行中です。終わってからやり直してください", http.StatusConflict},
	{service.ErrJudgeBusy, CodeConflict, "この Take はすでに Judge 中です", http.StatusConflict},
	{service.ErrNoVideo, CodeConflict, "この Take には動画ファイルがないため Judge できません", http.StatusConflict},
	{service.ErrNotJudged, CodeConflict, "この Take はまだ Judge されていません。Judge が終わってから Repair してください", http.StatusConflict},
	{service.ErrAccepted, CodeConflict, "この Take は Judge に合格しているため Repair は不要です", http.StatusConflict},
	{service.ErrBudgetExceeded, CodeBudget, "予算上限を超えるため生成できません", http.StatusPaymentRequired},
	{service.ErrLocked, CodeLocked, "LOCK されている項目は変更できません", http.StatusLocked},
	{service.ErrUnavailable, CodeUnavailable, "生成機能が有効になっていません（API キーと設定を確認してください）", http.StatusServiceUnavailable},
}
