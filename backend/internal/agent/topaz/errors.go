package topaz

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/logging"
)

// The errorCode values of the Video API that errmap tells apart (https://developer.topazlabs.com/getting-started/errors); the others of a failed
// job are all a failed job.
const (
	CodeServerMaintenance = "SERVER_MAINTENANCE"
	CodeCreditDifference  = "CREDIT_DIFFERENCE" //nolint:gosec // an error code of the API (the word "credit" is its price, not a credential)
	CodeInvalidInput      = "INVALID_INPUT"
	CodeInsufficient      = "INSUFFICIENT_CREDITS"
	CodeUnauthorized      = "UNAUTHORIZED"
	CodeForbidden         = "FORBIDDEN"
	CodeTimeoutUpload     = "TIMEOUT_UPLOAD_SOURCE"
	CodeTimeoutQueue      = "TIMEOUT_QUEUE"
	CodeTimeoutProcessing = "TIMEOUT_PROCESSING"
	// CodeUnknown is the errorCode of a failed job that names none (and the one Topaz sends for a failure it cannot classify).
	CodeUnknown = "UNKNOWN"
	// CodeCanceled is the code of a job whose status says it was cancelled: the answer has no errorCode then.
	CodeCanceled = "CANCELED"
	// CodeTransferFailed is the failure of mam's own transfer of a file to or from the storage URLs of Topaz (not the API): no errorCode of Topaz.
	CodeTransferFailed = "TRANSFER_FAILED"
)

// ErrNoAPIKey means no Topaz API key is stored.
var ErrNoAPIKey = errors.New("topaz: API key is not configured")

// Error is a failure that Topaz reported with an errorCode, or that happened in the transfer of a file to or from Topaz. errmap classifies it
// (apperr.CodeTopaz*, apperr.CodeCreditsInsufficient). Message never holds the API key.
type Error struct {
	// Code is the errorCode of Topaz (CodeCreditDifference, ...) or one of mam's own (CodeTransferFailed, CodeCanceled).
	Code string
	// Message is what Topaz said (the message of the answer) or what failed in the transfer.
	Message string
	// RequestID is the id of the job, for the support of Topaz ("" before there is one).
	RequestID string
	// Status is the HTTP status of the answer that carried the code; 0 for the failure of a job found by polling or of a transfer.
	Status int
	// Err is the failure behind: the *modeladapter.ProviderError of an answer, or the error of the transfer.
	Err error
}

func (e *Error) Error() string {
	s := "topaz: " + e.Code
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.RequestID != "" {
		s += " (requestId " + e.RequestID + ")"
	}
	return s
}

// Unwrap is the failure behind e.
func (e *Error) Unwrap() error { return e.Err }

// answer is the part of an error body (and of a status answer) that holds why a call failed.
type answer struct {
	ErrorCode string          `json:"errorCode"`
	Message   string          `json:"message"`
	Errors    json.RawMessage `json:"errors"`
}

// apiError is the failure of an answer with a status other than 2xx. It is a *modeladapter.ProviderError (the status classifies it and the
// Retry-After is kept), wrapped in an *Error when the body names an errorCode. Everything of the body is redacted: Topaz may echo a URL.
func (c *Client) apiError(res *http.Response, body []byte, key string) error {
	var a answer
	_ = json.Unmarshal(body, &a)
	msg := a.Message
	if msg == "" {
		msg = httpclient.Snippet(body)
	}
	if len(a.Errors) > 0 {
		msg += " " + httpclient.Snippet(a.Errors)
	}
	msg = strings.TrimSpace(logging.Redact(msg, key))
	pe := &modeladapter.ProviderError{
		Vendor: vendor, Status: res.StatusCode, Message: msg,
		RetryAfter: httpclient.ParseRetryAfter(res.Header.Get("Retry-After")),
	}
	if pe.RetryAfter > 0 {
		pe.Message += fmt.Sprintf(" (Retry-After: %ds)", int(pe.RetryAfter.Seconds()))
	}
	if a.ErrorCode == "" {
		return pe
	}
	return &Error{Code: a.ErrorCode, Message: msg, Status: res.StatusCode, Err: pe}
}

// badAnswer is an answer of Topaz that lacks what the call needs (a quote with no credits, an accept with no upload URL): not a failure of
// the network or of a status, so it is worded here.
func badAnswer(what string) error {
	return apperr.New(apperr.CodeUpstream).
		WithCause("Topaz の答えに、必要な情報（" + what + "）がありませんでした。Topaz の API の返す形が変わった可能性があります。").
		WithDetail("the answer of Topaz has no " + what)
}

// unsupported is a request that Topaz cannot do, found before asking it.
func unsupported(cause string) error {
	return apperr.New(apperr.CodeInvalidInput).WithCause(cause)
}

// transferFailed is the failure of a transfer to or from the storage URL of Topaz. The error underneath is the network's, and carries no
// URL (httpclient.RedactURL): a presigned URL is a credential.
func transferFailed(requestID, what string, err error) error {
	return &Error{Code: CodeTransferFailed, Message: what, RequestID: requestID, Err: err}
}
