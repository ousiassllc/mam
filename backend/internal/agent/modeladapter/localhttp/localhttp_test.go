package localhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

func newClient(t *testing.T, base string) *localhttp.Client {
	t.Helper()
	c, err := localhttp.New("ep", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: base + "/"}, credentialtest.New(), t.TempDir())
	require.NoError(t, err)
	return c
}

// Link-local is refused even for an endpoint the user registered, for requests and for downloads.
func TestLinkLocalIsRefusedEvenWhenRegistered(t *testing.T) {
	for _, base := range []string{"http://169.254.169.254", "http://[fe80::1]:8188", "http://[::ffff:169.254.169.254]"} {
		c := newClient(t, base)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, err := c.Request(ctx, http.MethodGet, "/latest/meta-data", nil, nil, 1<<20)
		require.Error(t, err, base)
		assert.False(t, modeladapter.IsUnavailable(err), "refused, not an outage: %v", err)
		assert.Contains(t, err.Error(), "refusing to connect", base)

		_, _, err = c.Download(ctx, c.Base+"/file", false, 1<<20)
		require.Error(t, err, base)
		assert.Contains(t, err.Error(), "refusing to connect", base)
		cancel()
	}
}

func TestRegisteredLoopbackAndPlainHTTPWork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`ok`)) }))
	defer srv.Close()
	c := newClient(t, srv.URL)
	data, err := c.Request(t.Context(), http.MethodGet, "/system_stats", nil, nil, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(data))
	blob, _, err := c.Download(t.Context(), c.Base+"/view?filename=a.png", false, 1<<20)
	require.NoError(t, err)
	got, _ := blob.Bytes()
	assert.Equal(t, "ok", string(got))
}

func TestAnswerLimitsAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			_, _ = w.Write(make([]byte, 100))
			return
		}
		w.Header().Set("Retry-After", "7")
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	_, err := c.Request(t.Context(), http.MethodGet, "/big", nil, nil, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")

	_, err = c.Request(t.Context(), http.MethodGet, "/busy", nil, nil, 10)
	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 7*time.Second, pe.RetryAfter)
	assert.True(t, modeladapter.RateLimited(err))

	_, _, err = c.Download(t.Context(), c.Base+"/big", false, 10)
	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrAfterSubmit)
}

func TestKeyOnlyUnderTheBaseURL(t *testing.T) {
	c := newClient(t, "http://100.64.1.2:8188")
	require.NoError(t, c.Keys.Set("endpoint:ep", "sk"))
	h, err := c.Auth("http://100.64.1.2:8188/view")
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk", h["Authorization"])
	for _, other := range []string{"http://100.64.1.2:8189/view", "https://100.64.1.2:8188/view", "http://100.64.1.3:8188/view", "http://elsewhere.example/view"} {
		h, err := c.Auth(other)
		require.NoError(t, err)
		assert.Empty(t, h, other)
	}
}
