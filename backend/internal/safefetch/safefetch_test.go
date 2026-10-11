package safefetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

func TestCheckURLAcceptsOnlyHTTPSURLsOfPublicHosts(t *testing.T) {
	for _, ok := range []string{"https://cdn.example.com/a.mp4", "https://8.8.8.8/a", "https://[2606:4700::1111]/a"} {
		assert.NoError(t, safefetch.CheckURL(ok), ok)
	}
	for _, bad := range []string{
		"http://cdn.example.com/a.mp4", "file:///etc/passwd", "ftp://example.com/a", "https:///a", "%zz",
		"https://127.0.0.1/a", "https://localhost/a", "https://LOCALHOST./a", "https://x.localhost/a",
		"https://169.254.169.254/latest/meta-data", "https://10.0.0.5/a", "https://192.168.1.1/a",
		"https://172.16.0.1/a", "https://0.0.0.0/a", "https://100.64.0.1/a", "https://[::1]/a",
		"https://[fe80::1]/a", "https://[fd00::1]/a", "https://[::ffff:127.0.0.1]/a",
	} {
		assert.Error(t, safefetch.CheckURL(bad), bad)
	}
}

func TestIsPublic(t *testing.T) {
	assert.True(t, safefetch.IsPublic(netip.MustParseAddr("93.184.216.34")))
	assert.False(t, safefetch.IsPublic(netip.MustParseAddr("::ffff:10.1.2.3")))
	assert.False(t, safefetch.IsPublic(netip.Addr{}))
}

// 6to4 (2002::/16) and well-known NAT64 (64:ff9b::/96) carry an IPv4 address. A private one must
// not count as public, while a public one still does.
func TestIsPublicRejectsEmbeddedNonPublicIPv4(t *testing.T) {
	for _, ip := range []string{
		"2002:a9fe:a9fe::",   // 169.254.169.254
		"2002:7f00:1::",      // 127.0.0.1
		"2002:a00:1::",       // 10.0.0.1
		"64:ff9b::a9fe:a9fe", // 169.254.169.254
		"64:ff9b::a00:1",     // 10.0.0.1
	} {
		require.False(t, safefetch.IsPublic(netip.MustParseAddr(ip)), ip)
		require.Error(t, safefetch.CheckURL("https://["+ip+"]/a"), ip)
	}
	assert.True(t, safefetch.IsPublic(netip.MustParseAddr("2002:5db8:d822::")), "6to4 of 93.184.216.34")
	assert.True(t, safefetch.IsPublic(netip.MustParseAddr("64:ff9b::808:808")), "NAT64 of 8.8.8.8")
}

// Ranges the IANA special-purpose registries set aside (IPv4-compatible, Teredo, local-use NAT64,
// documentation, discard, deprecated 6to4 relay) are not the public internet, whatever they embed.
func TestIsPublicRejectsSpecialPurposeRanges(t *testing.T) {
	for _, ip := range []string{
		"::7f00:1",                             // IPv4-compatible 127.0.0.1
		"::a00:1",                              // IPv4-compatible 10.0.0.1
		"::",                                   // unspecified
		"64:ff9b:1::a00:1",                     // local-use NAT64 (RFC 8215)
		"64:ff9b:1:ffff:ffff:ffff:ffff:ffff",   // end of the local-use NAT64 /48
		"2001:0:4136:e378:8000:63bf:3fff:fdd2", // Teredo
		"2001:db8::1",                          // documentation
		"100::1",                               // discard-only
		"192.88.99.1",                          // deprecated 6to4 relay
		"192.0.2.1",                            // documentation (TEST-NET-1)
		"198.51.100.1",                         // documentation (TEST-NET-2)
		"203.0.113.1",                          // documentation (TEST-NET-3)
	} {
		require.False(t, safefetch.IsPublic(netip.MustParseAddr(ip)), ip)
	}
	assert.True(t, safefetch.IsPublic(netip.MustParseAddr("::808:808")), "IPv4-compatible of 8.8.8.8")
	assert.True(t, safefetch.IsPublic(netip.MustParseAddr("2001:4860:4860::8888")), "public IPv6 near 2001::/32")
	assert.True(t, safefetch.IsPublic(netip.MustParseAddr("64:ff9b:2::1")), "outside the local-use NAT64 /48")
}

func TestCheckURLRejectsSpecialPurposeHosts(t *testing.T) {
	for _, host := range []string{
		"[::7f00:1]", "[::a00:1]", "[64:ff9b:1::a00:1]", "[2001:0:4136:e378:8000:63bf:3fff:fdd2]",
		"[2001:db8::1]", "[100::1]", "192.88.99.1", "192.0.2.1", "198.51.100.1", "203.0.113.1",
	} {
		assert.Error(t, safefetch.CheckURL("https://"+host+"/a"), host)
	}
}

func TestClientRefusesAURLThatResolvesToEmbeddedNonPublicIPv4(t *testing.T) {
	client := safefetch.NewClient()
	for _, ip := range []string{
		"2002:a9fe:a9fe::",
		"2002:7f00:1::",
		"2002:a00:1::",
		"64:ff9b::a9fe:a9fe",
		"64:ff9b::a00:1",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://["+ip+"]/", nil)
		require.NoError(t, err)
		_, err = client.Do(req)
		cancel()
		require.Error(t, err, ip)
		assert.Contains(t, err.Error(), "non-public address", ip)
	}
}

func TestClientDoesNotConnectToALoopbackServer(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	_, err = safefetch.NewClient().Do(req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-public address")
	assert.Zero(t, hits)
}
