package mcpkit

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
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

// ErrorCodes returns every code, in the order of the constants.
func ErrorCodes() []string { return slices.Clone(errorCodes) }

// ToolError is the error of a tool call as the agent sees it.
type ToolError struct {
	Code    string
	Message string
	// Status is the HTTP status the file endpoint answers with for the same error.
	Status int
	// ErrorID is the correlation id of an internal error (see ReportError); "" for any other.
	ErrorID string
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
		return &ToolError{Code: CodeForbidden, Message: "Brand / Safety Constraint に違反しています: " + brand.Message(), Status: http.StatusForbidden}
	case errors.Is(err, service.ErrBrandViolation):
		return &ToolError{Code: CodeForbidden, Message: "Brand / Safety Constraint に違反しています", Status: http.StatusForbidden}
	case errors.As(err, &conflicted):
		return &ToolError{Code: CodeConflict, Message: conflicted.Message, Status: http.StatusConflict}
	case errors.As(err, &locked) && len(locked.Parameters) > 0:
		names := make([]string, len(locked.Parameters))
		for i, p := range locked.Parameters {
			names[i] = p.Label()
		}
		return &ToolError{Code: CodeLocked, Message: strings.Join(names, " / ") + " は LOCK されているため変更できません。LOCK を解除してから指示してください", Status: http.StatusLocked}
	case errors.Is(err, service.ErrRateLimited):
		if errors.As(err, &ai) {
			return &ToolError{Code: CodeRateLimited, Message: ai.Reason, Status: http.StatusTooManyRequests}
		}
		return &ToolError{Code: CodeRateLimited, Message: "外部の生成モデル API のレート制限に達しました。しばらく待ってから再実行してください", Status: http.StatusTooManyRequests}
	case errors.As(err, &ai):
		return &ToolError{Code: CodeUpstream, Message: ai.Reason, Status: http.StatusBadGateway}
	case errors.As(err, &invalid):
		return &ToolError{Code: CodeValidation, Message: invalid.Error(), Status: http.StatusUnprocessableEntity}
	}
	for _, m := range sentinels {
		if errors.Is(err, m.err) {
			return &ToolError{Code: m.code, Message: m.message, Status: m.status}
		}
	}
	return &ToolError{Code: CodeInternal, Message: "予期しないエラーです。mam のログを確認してください", Status: http.StatusInternalServerError}
}

// ErrorIDKey is the attribute of the log line that holds the correlation id of an internal error.
const ErrorIDKey = "error_id"

// NewErrorID returns the correlation id of one internal failure.
func NewErrorID() string { return "err_" + domain.NewID() }

// ReportError maps err for the caller. An internal error (one MapError does not know) gets a new
// correlation id: its cause is logged at once with the id as the error_id attribute, and the returned
// error tells the id, so that an agent finds the cause with get_app_logs (attrs: error_id) without the
// cause being in the message. attrs are added to the log line. An error that already carries an id was
// reported before and is returned as it is.
func ReportError(logger *slog.Logger, msg string, err error, attrs ...any) *ToolError {
	te := MapError(err)
	if te.Code != CodeInternal || te.ErrorID != "" {
		return te
	}
	id := NewErrorID()
	if logger != nil {
		logger.Error(msg, append([]any{ErrorIDKey, id, "err", err}, attrs...)...)
	}
	return te.WithErrorID(id)
}

// WithErrorID returns a copy of the internal error that carries id in the error and in its message.
func (e *ToolError) WithErrorID(id string) *ToolError {
	c := *e
	c.ErrorID = id
	c.Message = fmt.Sprintf("%s（%s: %s）", e.Message, ErrorIDKey, id)
	return &c
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
