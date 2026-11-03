package localmodels_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

func TestConnectionTest(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, false, "")

	comfyUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/system_stats" {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.3.60"},"devices":[{"name":"cuda:0 NVIDIA RTX"}]}`))
	}))
	defer comfyUp.Close()
	res, err := r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindComfyUI, BaseURL: comfyUp.URL})
	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Equal(t, "0.3.60", res.Version)
	assert.Empty(t, res.Note)

	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.3.40"},"devices":[]}`))
	}))
	defer old.Close()
	res, _ = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindComfyUI, BaseURL: old.URL})
	assert.True(t, res.OK)
	assert.Contains(t, res.Note, "0.3.57", "an old ComfyUI cannot cancel one job safely")

	notComfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) }))
	defer notComfy.Close()
	res, _ = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindComfyUI, BaseURL: notComfy.URL})
	assert.False(t, res.OK)

	compat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"flux-dev"},{"id":"sdxl"}]}`))
	}))
	defer compat.Close()
	res, _ = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindOpenAICompat, BaseURL: compat.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "認証")
	res, _ = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindOpenAICompat, BaseURL: compat.URL, APIKey: "good"})
	assert.True(t, res.OK)
	assert.Equal(t, []string{"flux-dev", "sdxl"}, res.Models)
	ok, _ := r.keys.Has("endpoint:draft")
	assert.False(t, ok, "a draft key is never saved")

	// A server without /v1/models is reachable: OK with a note, not a failure.
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { http.NotFound(w, req) }))
	defer bare.Close()
	res, _ = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindOpenAICompat, BaseURL: bare.URL})
	assert.True(t, res.OK)
	assert.Contains(t, res.Note, "/v1/models")

	// ComfyUI that answers 404 is not ComfyUI.
	res, _ = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindComfyUI, BaseURL: bare.URL})
	assert.False(t, res.OK)

	// Nothing listens there.
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	res, _ = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindComfyUI, BaseURL: url})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "接続できませんでした")

	// Link-local is refused even though the user typed it.
	var invalid *localmodels.InvalidError
	_, err = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindComfyUI, BaseURL: "http://169.254.169.254"})
	require.ErrorAs(t, err, &invalid)

	// A saved endpoint is tested with its saved key.
	require.NoError(t, r.svc.CreateEndpoint(ctx, localmodels.EndpointInput{Name: "oa", Kind: localcfg.KindOpenAICompat, BaseURL: compat.URL}))
	require.NoError(t, r.svc.SetEndpointKey("oa", "good"))
	res, err = r.svc.TestEndpoint(ctx, "oa")
	require.NoError(t, err)
	assert.True(t, res.OK)
	_, err = r.svc.TestEndpoint(ctx, "missing")
	assert.ErrorIs(t, err, localmodels.ErrNotFound)
}

// The saved key of an endpoint is sent only to the address it was saved for: a draft of the same name with another address (the form, or an MCP call)
// is tested without it.
func TestConnectionTestDoesNotSendTheSavedKeyToAnotherAddress(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, false, "")
	var mu sync.Mutex
	seen := map[string][]string{}
	serve := func(name string) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mu.Lock()
			seen[name] = append(seen[name], req.Header.Get("Authorization"))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
		}))
		t.Cleanup(s.Close)
		return s
	}
	saved, other := serve("saved"), serve("other")
	require.NoError(t, r.svc.CreateEndpoint(ctx, localmodels.EndpointInput{Name: "oa", Kind: localcfg.KindOpenAICompat, BaseURL: saved.URL}))
	require.NoError(t, r.svc.SetEndpointKey("oa", "saved-key"))

	_, err := r.svc.Test(ctx, localmodels.TestInput{Name: "oa", Kind: localcfg.KindOpenAICompat, BaseURL: other.URL})
	require.NoError(t, err)
	_, err = r.svc.Test(ctx, localmodels.TestInput{Name: "oa", Kind: localcfg.KindOpenAICompat, BaseURL: saved.URL + "/"})
	require.NoError(t, err)
	_, err = r.svc.Test(ctx, localmodels.TestInput{Kind: localcfg.KindOpenAICompat, BaseURL: other.URL})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"", ""}, seen["other"], "the saved key does not go to another address")
	assert.Equal(t, []string{"Bearer saved-key"}, seen["saved"], "the saved address keeps its key")
}
