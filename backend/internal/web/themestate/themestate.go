// Package themestate is where the colour theme chosen with the theme button lives between requests: a cookie that
// static/src/components/theme writes, and that Middleware reads into the request context so that <html> carries its data-theme
// from the first byte (no flash of the other theme). Without the cookie the page follows the system (prefers-color-scheme in
// static/src/css/tokens.css). A cookie rather than localStorage for the reason of navstate: the desktop app's loopback port
// changes from run to run.
package themestate

import (
	"context"

	"github.com/gin-gonic/gin"
)

const (
	// CookieName is the cookie that holds the theme.
	CookieName = "mam_theme"
	// Light is a value of the cookie (and of the data-theme attribute); anything but Light and Dark is no choice.
	Light = "light"
	// Dark is the other value of the cookie (and of the data-theme attribute).
	Dark = "dark"
	// MaxAgeSeconds is how long the choice is kept: a year.
	MaxAgeSeconds = 365 * 24 * 60 * 60
)

type ctxKey struct{}

// With is ctx that says which theme the user chose: Light, Dark, or "" for none (follow the system).
func With(ctx context.Context, theme string) context.Context {
	return context.WithValue(ctx, ctxKey{}, known(theme))
}

// Chosen is the theme of the request that ctx belongs to: Light, Dark, or "" when the user has not chosen one.
func Chosen(ctx context.Context) string {
	theme, _ := ctx.Value(ctxKey{}).(string)
	return theme
}

func known(theme string) string {
	if theme == Light || theme == Dark {
		return theme
	}
	return ""
}

// Middleware reads CookieName into the request context (read by Chosen).
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		value, _ := c.Cookie(CookieName)
		c.Request = c.Request.WithContext(With(c.Request.Context(), value))
		c.Next()
	}
}
