package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// NetworkError is a failure to get an answer (connection, timeout, an unreadable body). It is never a reason to
// delete tokens.
type NetworkError struct{ Err error }

func (e *NetworkError) Error() string {
	return "oauth: no usable answer from the authorization server: " + e.Err.Error()
}
func (e *NetworkError) Unwrap() error { return e.Err }

// TokenError is an HTTP error answer of the authorization server. Its text holds the status and the error code
// only: the body may echo more than a log should keep.
type TokenError struct {
	Status int
	// Code is the OAuth error code ("invalid_grant", "refresh_token_expired", …); empty when the body has none.
	Code string
}

func (e *TokenError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("oauth: the authorization server answered HTTP %d", e.Status)
	}
	return fmt.Sprintf("oauth: the authorization server answered HTTP %d (%s)", e.Status, e.Code)
}

func newTokenError(status int, body []byte) *TokenError {
	return &TokenError{Status: status, Code: errorCode(body)}
}

// errorCode reads the code of the shapes the server uses: {"error":"invalid_grant"},
// {"error":{"code":"refresh_token_expired"}} and {"code":"…"}.
func errorCode(body []byte) string {
	var env struct {
		Error json.RawMessage `json:"error"`
		Code  string          `json:"code"`
	}
	if json.Unmarshal(body, &env) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(env.Error, &s) == nil && s != "" {
		return s
	}
	var obj struct {
		Code string `json:"code"`
		Type string `json:"type"`
	}
	if json.Unmarshal(env.Error, &obj) == nil {
		if obj.Code != "" {
			return obj.Code
		}
		if obj.Type != "" {
			return obj.Type
		}
	}
	return env.Code
}

// reloginCodes are the answers to a refresh that mean the refresh token is gone for good (SIWC errors-and-recovery,
// Codex refresh errors). Only a new sign-in helps.
var reloginCodes = []string{
	"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired",
	"refresh_token_invalidated", "refresh_token_reused",
}

// ReloginRequired reports whether err, the failure of a refresh, says the sign-in must be repeated, and the
// code that says so. A network failure, a 5xx and an unknown 4xx never do: the tokens are kept.
func ReloginRequired(err error) (code string, ok bool) {
	var te *TokenError
	if !errors.As(err, &te) || te.Status < 400 || te.Status >= 500 {
		return "", false
	}
	if slices.Contains(reloginCodes, te.Code) {
		return te.Code, true
	}
	return "", false
}
