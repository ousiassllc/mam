// Package navstate is where the side menu's folded / open state lives between requests: a cookie that static/src
// components/sidenav writes when the menu is folded or opened, and that Middleware reads into the request context so that the
// page is drawn folded from the first byte (no flash of the open menu). A cookie rather than localStorage: the desktop app
// (Wails) loads the loopback URL on a port that may differ from run to run, a cookie is shared across ports and
// localStorage is not (spec/components/side-menu.md §4).
package navstate

import (
	"context"

	"github.com/gin-gonic/gin"
)

const (
	// CookieName is the cookie that holds the state.
	CookieName = "mam_nav"
	// Folded is the value of the cookie when the menu is folded; anything else (or no cookie) is open.
	Folded = "folded"
	// MaxAgeSeconds is how long the choice is kept: a year.
	MaxAgeSeconds = 365 * 24 * 60 * 60
)

type ctxKey struct{}

// With is ctx that says whether the menu is folded.
func With(ctx context.Context, folded bool) context.Context {
	return context.WithValue(ctx, ctxKey{}, folded)
}

// IsFolded reports whether the menu of the request that ctx belongs to is folded; no state means open.
func IsFolded(ctx context.Context) bool {
	folded, _ := ctx.Value(ctxKey{}).(bool)
	return folded
}

// Middleware reads CookieName into the request context (read by IsFolded).
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		value, err := c.Cookie(CookieName)
		c.Request = c.Request.WithContext(With(c.Request.Context(), err == nil && value == Folded))
		c.Next()
	}
}
