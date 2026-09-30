// Package httpclient builds the HTTP client every vendor adapter uses when none is injected.
// The adapters authenticate with their own header (x-api-key, xi-api-key, x-goog-api-key, x-key),
// which net/http would copy to the target of a redirect to another host, unlike Authorization.
package httpclient

import (
	"errors"
	"net"
	"net/http"
	"time"
)

// Doer is the part of *http.Client the adapters use; tests inject their own.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

const (
	dialTimeout = 10 * time.Second
	// headerTimeout is only a backstop against a vendor that accepts the request and never answers:
	// the caller's context is what bounds a call (a synchronous image call legitimately takes minutes).
	headerTimeout = 10 * time.Minute
	maxRedirects  = 10
)

// New returns a client that refuses to follow a redirect to another host (so a header carrying an
// API key never reaches it) or from https to http, and whose transport times out a stalled
// connect, TLS handshake or response header. It has no overall timeout: the caller's context
// bounds a call.
func New() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = dialTimeout
	tr.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Transport: tr, CheckRedirect: sameHost}
}

// sameHost is the CheckRedirect of New.
func sameHost(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("stopped after too many redirects")
	}
	first := via[0]
	if req.URL.Host != first.URL.Host {
		return errors.New("refusing to follow a redirect to another host")
	}
	if first.URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("refusing to follow a redirect from https to http")
	}
	return nil
}
