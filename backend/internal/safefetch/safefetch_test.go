package safefetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

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
