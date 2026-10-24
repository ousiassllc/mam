package httpclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

func get(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("x-api-key", "secret")
	return httpclient.New().Do(req)
}

func TestRedirectToAnotherHostIsRefusedAndNeverCarriesTheKey(t *testing.T) {
	var foreign atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreign.Add(1) }))
	defer other.Close()
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	}))
	defer vendor.Close()

	res, err := get(t, vendor.URL)
	if res != nil {
		_ = res.Body.Close()
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "another host")
	assert.Zero(t, foreign.Load())
}

func TestRedirectWithinTheSameHostIsFollowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/b", http.StatusFound) })
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(r.Header.Get("x-api-key"))) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	res, err := get(t, srv.URL+"/a")

	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestRedirectLoopIsStopped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusFound) }))
	defer srv.Close()

	res, err := get(t, srv.URL)
	if res != nil {
		_ = res.Body.Close()
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many redirects")
}

// NewGuarded runs its control on the resolved address of every connection, so a name cannot smuggle an
// address in, and ignores proxy settings (a proxy would hide the destination from the guard).
func TestNewGuardedChecksEveryConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")

	var seen []string
	allow := func(_, address string, _ syscall.RawConn) error { seen = append(seen, address); return nil }
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	res, err := httpclient.NewGuarded(allow).Do(req)
	require.NoError(t, err, "the proxy setting is ignored")
	_ = res.Body.Close()
	require.Len(t, seen, 1)
	assert.Equal(t, srv.Listener.Addr().String(), seen[0])

	deny := func(_, _ string, _ syscall.RawConn) error { return errors.New("denied") }
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	_, err = httpclient.NewGuarded(deny).Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied")
}
