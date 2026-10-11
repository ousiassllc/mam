// Package middleware holds the HTTP middleware shared by every route.
package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	// CSRFCookieName holds the per-browser CSRF secret (HttpOnly, SameSite=Strict).
	CSRFCookieName = "mam_csrf"
	// CSRFHeaderName carries the token on HTMX, fetch and Huma API requests.
	CSRFHeaderName = "X-CSRF-Token"

	csrfContextKey = "mam.csrf_token"
	tokenBytes     = 32
)

// CSRF implements HALT's token model: every response sets the token cookie if
// missing and exposes the token to templates (rendered into a <meta> tag), and
// every state-changing request (anything but GET/HEAD/OPTIONS) MUST echo the
// token in the X-CSRF-Token header. Only the header is read, never the body: parsing a
// form here would buffer an unbounded upload before the handler can limit it.
// Requests without a matching token are rejected with 403.
func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(CSRFCookieName)
		fresh := err != nil || !validToken(token)
		if fresh {
			token, err = newToken()
			if err != nil {
				_ = c.Error(err)
				abortWith(c, http.StatusInternalServerError, "", "内部エラーが発生しました")
				return
			}
			setCSRFCookie(c, token)
		}
		c.Set(csrfContextKey, token)

		if isSafeMethod(c.Request.Method) {
			c.Next()
			return
		}
		if fresh || !tokensEqual(token, c.GetHeader(CSRFHeaderName)) {
			abortWith(c, http.StatusForbidden, "invalid or missing CSRF token", "送信を確認できませんでした。画面を再読み込みしてからやり直してください")
			return
		}
		c.Next()
	}
}

// CSRFToken returns the token for the current request, for the <meta name="csrf-token"> tag.
func CSRFToken(c *gin.Context) string {
	return c.GetString(csrfContextKey)
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func validToken(t string) bool {
	b, err := base64.RawURLEncoding.DecodeString(t)
	return err == nil && len(b) == tokenBytes
}

func tokensEqual(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func setCSRFCookie(c *gin.Context, token string) {
	//nolint:gosec // Secure cannot be set: the server is plain HTTP on the loopback interface
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
