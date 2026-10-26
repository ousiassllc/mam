package safefetch

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// This file is the relaxed policy for an endpoint the user registered on purpose (a ComfyUI or an
// OpenAI-compatible server on localhost, the LAN or a tailnet): plain http and the local, private and
// 100.64.0.0/10 addresses are fine for that endpoint's own origin, and nothing else changes. What is
// never fine, even for a registered endpoint, is link-local (169.254.0.0/16, fe80::/10: the cloud
// metadata service), the unspecified address and multicast.

// ErrRefused marks an address or URL the policy refuses; retrying it never helps.
var ErrRefused = errors.New("refused by the address policy")

// IsRefusedForEndpoint reports whether ip is an address no endpoint may be at: link-local, unspecified or
// multicast. An IPv4 address embedded in an IPv4-mapped, IPv4-compatible, 6to4 or NAT64 address is judged
// on its own.
func IsRefusedForEndpoint(ip netip.Addr) bool {
	ip = ip.Unmap()
	if v4, ok := embeddedIPv4(ip); ok {
		ip = v4
	}
	return !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast()
}

// guardEndpoint is the net.Dialer Control of a registered endpoint's client. It sees the address after
// name resolution and runs for every connection, so a registered host name that resolves (or is
// rebound) to a link-local address is stopped at connect time.
func guardEndpoint(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || IsRefusedForEndpoint(ip) {
		return fmt.Errorf("%w: refusing to connect to the address %s (link-local, unspecified and multicast addresses are never allowed)", ErrRefused, host)
	}
	return nil
}

// NewEndpointClient returns the client for requests to a registered endpoint: it connects to any
// address except those of IsRefusedForEndpoint, ignores proxy settings, and refuses a redirect to
// another host or from https to http (httpclient.New).
func NewEndpointClient() *http.Client { return httpclient.NewGuarded(guardEndpoint) }

// originPort is the port of u, the scheme's default when the URL names none.
func originPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// SameOrigin reports whether a and b have the same scheme, host and port (a default port equals the
// explicit one). A URL with a user name or password is never the same origin.
func SameOrigin(a, b *url.URL) bool {
	if a.User != nil || b.User != nil || a.Hostname() == "" || b.Hostname() == "" {
		return false
	}
	host := func(u *url.URL) string { return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".") }
	return strings.EqualFold(a.Scheme, b.Scheme) && host(a) == host(b) && originPort(a) == originPort(b)
}

// Within reports whether target is under base: the same origin, and a path that is base's path or goes
// below it (at a path segment boundary). It is the test for sending an API key.
func Within(target, base string) bool {
	t, err1 := url.Parse(target)
	b, err2 := url.Parse(base)
	if err1 != nil || err2 != nil || !SameOrigin(t, b) {
		return false
	}
	prefix := strings.TrimRight(b.Path, "/")
	return prefix == "" || t.Path == prefix || strings.HasPrefix(t.Path, prefix+"/")
}

// OriginClient downloads files whose URL came from a registered endpoint's answer. A URL of the
// endpoint's own origin goes through NewEndpointClient (http and local addresses allowed, link-local
// refused at connect time); any other URL must pass the rules for vendor result URLs (CheckURL: https, a
// public address, checked again at connect time by NewClient).
type OriginClient struct {
	base     *url.URL
	endpoint *http.Client
	public   *http.Client
}

// NewOriginClient returns the OriginClient of the endpoint at baseURL.
func NewOriginClient(baseURL string) (*OriginClient, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("the endpoint URL is not an http(s) URL")
	}
	return &OriginClient{base: base, endpoint: NewEndpointClient(), public: NewClient()}, nil
}

// Do sends req with the client its URL calls for.
func (c *OriginClient) Do(req *http.Request) (*http.Response, error) {
	if SameOrigin(req.URL, c.base) {
		return c.endpoint.Do(req) //nolint:gosec // a request to the registered endpoint, which the user entered
	}
	if err := CheckURL(req.URL.String()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRefused, err)
	}
	return c.public.Do(req) //nolint:gosec // CheckURL above refused anything but a public https URL
}
