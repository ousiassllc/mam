package middleware

import (
	"net"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// cspPrefix and cspSuffix surround the per-request connect-src source of SecurityHeaders.
//
//   - script-src 'self': the only script is paths.StaticApp (/static/app.js). No inline script, no eval, so a
//     <script> or an on* attribute that ever reaches a page or an HTMX fragment does not run.
//   - style-src 'unsafe-inline': the Lit components set style="left:…;width:…" attributes and htmx
//     injects its indicator <style>; style-src 'self' alone would drop both.
//   - img-src data: / media-src blob: cover inline icons and object-URL playback.
//   - frame-ancestors 'none': the app is never framed (clickjacking).
const (
	cspPrefix = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'"
	cspSuffix = "; img-src 'self' data:; media-src 'self' blob:; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
)

// SecurityHeaders sets Content-Security-Policy and X-Content-Type-Options on every response, so the
// "no script but our own paths.StaticApp" design of the front end (htmx-config.ts, AppLayout) is
// enforced by the browser and not only by convention.
//
// It sets the headers before the handler runs, with Header().Set, so a handler that sets its own
// (the asset and video routes send `Content-Security-Policy: sandbox`) replaces them.
//
// Older WebKit does not count a same-origin ws: URL as 'self', so the WebSocket origin of the
// request's Host (already checked by LoopbackOnly) is listed in connect-src as well.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy(c.Request.Host))
		h.Set("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func contentSecurityPolicy(host string) string {
	var b strings.Builder
	b.WriteString(cspPrefix)
	if ws := webSocketSource(host); ws != "" {
		b.WriteString(" ")
		b.WriteString(ws)
	}
	b.WriteString(cspSuffix)
	return b.String()
}

// webSocketSource returns the ws://host[:port] CSP source for a Host header, or "" when the
// header is not a plain host or host:port (it is copied into a header, so nothing else may pass).
func webSocketSource(hostport string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = strings.Trim(hostport, "[]"), ""
	}
	if !isLoopbackHost(host) {
		return ""
	}
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return ""
		}
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "ws://" + host
}
