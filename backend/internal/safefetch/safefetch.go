// Package safefetch guards the download of a file whose URL a vendor API answered with: the URL
// comes from outside, so it must not lead to a plain-http endpoint or to an address of the
// machine's own network (loopback, link-local such as the cloud metadata service, private ranges).
package safefetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// maxRedirects bounds the redirects one download follows.
const maxRedirects = 5

// blocked lists the ranges netip.Addr has no predicate for: "this" network, carrier-grade NAT,
// IETF protocol assignments, benchmarking and the reserved class E.
var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

// IsPublic reports whether ip is an address of the public internet.
func IsPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// CheckURL accepts raw only when it is an https URL and its host is neither "localhost" nor a
// non-public IP address. A host name that resolves to such an address is stopped at connection
// time by the client of NewClient.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return errors.New("the result URL is not a valid URL")
	}
	if u.Scheme != "https" {
		return errors.New("the result URL is not an https URL")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("the result URL points to the local machine")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !IsPublic(ip) {
		return errors.New("the result URL points to a non-public address")
	}
	return nil
}

// guard is the net.Dialer Control that refuses to connect to a non-public address. It sees the
// address after name resolution, so a host name cannot smuggle one in.
func guard(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !IsPublic(ip) {
		return fmt.Errorf("refusing to connect to the non-public address %s", host)
	}
	return nil
}

// NewClient returns an HTTP client for downloading vendor result files: it connects to public
// addresses only, follows a few redirects as long as they stay on https URLs that pass CheckURL,
// and ignores proxy settings (a proxy would hide the real destination from the address guard).
// It has no overall timeout: the caller's context bounds a download.
func NewClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		d := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: guard}
		return d.DialContext(ctx, network, addr)
	}
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return CheckURL(req.URL.String())
		},
	}
}
