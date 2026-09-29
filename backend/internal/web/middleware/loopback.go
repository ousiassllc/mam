package middleware

import (
	"net"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

// LoopbackOnly rejects requests that did not come from the local machine or that
// address the server by a non-loopback name. The listener is already bound to
// 127.0.0.1; this is defence in depth against DNS rebinding (a remote page whose
// hostname resolves to 127.0.0.1) and cross-site requests from other origins.
//
//   - RemoteAddr MUST be a loopback IP.
//   - Host MUST be an IP literal in the loopback range or "localhost".
//   - When present, Origin MUST have such a host as well.
func LoopbackOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isLoopbackAddr(c.Request.RemoteAddr) || !isLoopbackHost(c.Request.Host) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" && !isLoopbackOrigin(origin) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return isLoopbackHost(u.Host)
}
