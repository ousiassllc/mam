package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
