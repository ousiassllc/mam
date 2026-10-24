package middleware

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// frameSourcePattern is an origin a CSP frame-src may be given: scheme://host[:port], with a host of letters,
// digits, dots and hyphens or a bracketed IPv6 address. Nothing else may pass: the value is copied into a header.
var frameSourcePattern = regexp.MustCompile(`^https?://(\[[0-9a-fA-F:]+\]|[0-9A-Za-z]([0-9A-Za-z.-]*[0-9A-Za-z])?)(:[0-9]{1,5})?$`)

// AllowFrame adds frame-src for exactly one origin to the Content-Security-Policy of the response: the ComfyUI the
// node screen embeds (spec/architecture/comfy/comfy-embedding.md). Every other page keeps the policy without frame-src,
// so a frame falls back to default-src 'self' and no other site can be put into a page. It returns false, and
// changes nothing, for anything that is not a plain origin; call it before the page is written.
func AllowFrame(c *gin.Context, origin string) bool {
	if !frameSourcePattern.MatchString(origin) {
		return false
	}
	if u, err := url.Parse(origin); err != nil || u.Port() == "0" || len(u.Port()) > 0 && !validPort(u.Port()) {
		return false
	}
	h := c.Writer.Header()
	csp := h.Get("Content-Security-Policy")
	if !strings.Contains(csp, "; frame-ancestors ") || strings.Contains(csp, "frame-src") {
		return false // not the policy SecurityHeaders set (a handler replaced it), or already allowed
	}
	h.Set("Content-Security-Policy", strings.Replace(csp, "; frame-ancestors ", "; frame-src "+origin+"; frame-ancestors ", 1))
	return true
}

func validPort(p string) bool {
	n := 0
	for _, r := range p {
		n = n*10 + int(r-'0')
	}
	return n >= 1 && n <= 65535
}
