package protocol

import (
	"bytes"
	"encoding/json"
)

// The codexErrorInfo kinds mam tells apart. The unit variants arrive as a string, the ones that carry data as an
// object with one key ({"httpConnectionFailed":{"httpStatusCode":502}}); both decode to ErrorInfo.Kind.
const (
	ErrContextWindowExceeded        = "contextWindowExceeded"
	ErrUsageLimitExceeded           = "usageLimitExceeded"
	ErrUnauthorized                 = "unauthorized"
	ErrBadRequest                   = "badRequest"
	ErrSandboxError                 = "sandboxError"
	ErrInternalServerError          = "internalServerError"
	ErrOther                        = "other"
	ErrServerOverloaded             = "serverOverloaded"
	ErrRateLimitExceeded            = "rateLimitExceeded"
	ErrHTTPConnectionFailed         = "httpConnectionFailed"
	ErrResponseStreamConnectFailed  = "responseStreamConnectionFailed"
	ErrResponseStreamDisconnected   = "responseStreamDisconnected"
	ErrResponseTooManyFailedAttempt = "responseTooManyFailedAttempts"
)

// ErrorInfo is a codexErrorInfo. The zero value is "no info".
type ErrorInfo struct {
	Kind       string
	HTTPStatus int
}

// UnmarshalJSON reads the string form and the one-key object form; anything else is Kind "other".
func (e *ErrorInfo) UnmarshalJSON(b []byte) error {
	*e = ErrorInfo{}
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		e.Kind = s
		return nil
	}
	var obj map[string]struct {
		HTTPStatusCode *int `json:"httpStatusCode"`
	}
	if json.Unmarshal(b, &obj) != nil || len(obj) != 1 {
		e.Kind = ErrOther
		return nil
	}
	for k, v := range obj {
		e.Kind = k
		if v.HTTPStatusCode != nil {
			e.HTTPStatus = *v.HTTPStatusCode
		}
	}
	return nil
}
