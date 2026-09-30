package middleware

import (
	"net"
	"net/http"
	"net/url"
	"strings"

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
//   - On a state-changing request (not GET/HEAD/OPTIONS) or a WebSocket handshake, Origin MUST also
//     be the origin of this server, i.e. its host:port equals Host: another page on 127.0.0.1 or
//     localhost (a dev server on another port) is a different origin.
func LoopbackOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isLoopbackAddr(c.Request.RemoteAddr) || !isLoopbackHost(c.Request.Host) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" && !isLoopbackOrigin(origin) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		} else if origin != "" && needsSameOrigin(c.Request) && !isSameOrigin(origin, c.Request.Host) {
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
	if strings.EqualFold(host, "localhost") {
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

// needsSameOrigin reports whether the request changes state or opens a WebSocket: the requests a
// page of another origin must not be able to make.
func needsSameOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	}
	return true
}

func isSameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, host)
}
