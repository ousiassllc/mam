package safefetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

func TestIsRefusedForEndpoint(t *testing.T) {
	for _, ip := range []string{
		"169.254.169.254", "169.254.0.1", "fe80::1", "::ffff:169.254.169.254", "0.0.0.0", "::", "224.0.0.1", "ff02::1",
		"2002:a9fe:a9fe::",   // 6to4 of 169.254.169.254
		"64:ff9b::a9fe:a9fe", // NAT64 of 169.254.169.254
	} {
		assert.True(t, safefetch.IsRefusedForEndpoint(netip.MustParseAddr(ip)), ip)
	}
	for _, ip := range []string{"127.0.0.1", "::1", "10.1.2.3", "192.168.0.7", "172.16.5.5", "100.64.0.1", "100.127.255.254", "fd7a:115c:a1e0::1", "93.184.216.34"} {
		assert.False(t, safefetch.IsRefusedForEndpoint(netip.MustParseAddr(ip)), ip)
	}
}

func TestSameOriginAndWithin(t *testing.T) {
	assert.True(t, safefetch.Within("http://100.64.1.2:8188/view?x=1", "http://100.64.1.2:8188"))
	assert.True(t, safefetch.Within("https://Host.Example/api/v1/a", "https://host.example:443/api"))
	assert.True(t, safefetch.Within("http://h:80/", "http://h"))
	assert.False(t, safefetch.Within("http://h:8189/", "http://h:8188"), "another port")
	assert.False(t, safefetch.Within("https://h:8188/", "http://h:8188"), "another scheme")
	assert.False(t, safefetch.Within("http://other:8188/", "http://h:8188"), "another host")
	assert.False(t, safefetch.Within("http://h.evil:8188/", "http://h:8188"))
	assert.False(t, safefetch.Within("http://u:p@h:8188/", "http://h:8188"), "user info")
	assert.False(t, safefetch.Within("http://h:8188/apix", "http://h:8188/api"), "a path prefix must end at a segment")
	assert.False(t, safefetch.Within("http://h:8188/other", "http://h:8188/api"))
	u1, _ := url.Parse("http://h:8188")
	u2, _ := url.Parse("http://h:8188/anything")
	assert.True(t, safefetch.SameOrigin(u1, u2))
}

func get(t *testing.T, c interface {
	Do(*http.Request) (*http.Response, error)
}, target string) (*http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	require.NoError(t, err)
	return c.Do(req)
}

func TestOriginClientAllowsTheRegisteredOriginOnPlainHTTPAndLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	c, err := safefetch.NewOriginClient(srv.URL)
	require.NoError(t, err)

	res, err := get(t, c, srv.URL+"/view?filename=a.png")
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

// Anything that is not the registered origin keeps the vendor rules: https and a public address only.
func TestOriginClientKeepsTheVendorRulesForOtherURLs(t *testing.T) {
	registered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer registered.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("secret")) }))
	defer other.Close()
	c, err := safefetch.NewOriginClient(registered.URL)
	require.NoError(t, err)

	for _, target := range []string{
		other.URL + "/a",                 // another port of localhost, plain http
		"http://127.0.0.1:1/a",           // loopback, http
		"https://127.0.0.1:1/a",          // loopback, https
		"https://localhost:1/a",          // localhost
		"https://10.0.0.5/a",             // private
		"https://192.168.1.1/a",          // private
		"https://100.64.0.1/a",           // tailnet / CGNAT
		"https://[::1]/a",                // IPv6 loopback
		"http://cdn.example.com/a.png",   // public host, plain http
		"https://169.254.169.254/latest", // metadata service
	} {
		_, err := get(t, c, target)
		assert.Error(t, err, target)
	}
}

// Link-local is refused for the registered origin too, by the address the connection is made to.
func TestEndpointClientNeverConnectsToLinkLocal(t *testing.T) {
	for _, target := range []string{"http://169.254.169.254/latest/meta-data", "http://[fe80::1]:8188/system_stats", "http://[::ffff:169.254.169.254]/"} {
		c, err := safefetch.NewOriginClient(target)
		require.NoError(t, err)
		_, err = get(t, c, target)
		require.Error(t, err, target)
		assert.Contains(t, err.Error(), "refusing to connect", target)
		_, err = get(t, safefetch.NewEndpointClient(), target)
		require.Error(t, err, target)
		assert.Contains(t, err.Error(), "refusing to connect", target)
	}
}

func TestEndpointClientRefusesRedirectsToAnotherHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("elsewhere")) }))
	defer target.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x", http.StatusFound)
	}))
	defer redirecting.Close()

	c, err := safefetch.NewOriginClient(redirecting.URL)
	require.NoError(t, err)
	_, err = get(t, c, redirecting.URL+"/a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "another host")
}

func TestNewOriginClientRejectsNonHTTPURLs(t *testing.T) {
	for _, bad := range []string{"", "ftp://h/", "file:///x", "http://", "%zz"} {
		_, err := safefetch.NewOriginClient(bad)
		assert.Error(t, err, bad)
	}
}
