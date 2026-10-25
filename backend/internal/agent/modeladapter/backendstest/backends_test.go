package backendstest_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type memKeys map[string]string

func (m memKeys) Set(p, k string) error { m[p] = k; return nil }
func (m memKeys) Get(p string) (string, error) {
	if k, ok := m[p]; ok {
		return k, nil
	}
	return "", credential.ErrNotFound
}
func (m memKeys) Delete(p string) error      { delete(m, p); return nil }
func (m memKeys) Has(p string) (bool, error) { _, ok := m[p]; return ok, nil }

func backendsFor(t *testing.T, srv *httptest.Server, keys memKeys) modeladapter.Backends {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	return modeladapter.NewBackends(cfg, keys, srv.Client(), time.Millisecond, modeladapter.WithLocalDownloads())
}

// strictBackendsFor is backendsFor without WithLocalDownloads: result URLs must be https URLs of
// public hosts. client records every request it is asked to send.
func strictBackendsFor(t *testing.T, srv *httptest.Server, keys memKeys, sent *[]string) modeladapter.Backends {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	return modeladapter.NewBackends(cfg, keys, doerFunc(func(r *http.Request) (*http.Response, error) {
		*sent = append(*sent, r.URL.String())
		return srv.Client().Do(r)
	}), time.Millisecond)
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestImageBackendWithoutAPIKeyFailsBeforeAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{})

	_, err := b.Image[domain.ModelGPTImage2].GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "x"})

	require.ErrorIs(t, err, modeladapter.ErrNoAPIKey)
	assert.Zero(t, hits.Load())
}

func TestGeminiImageBackendSendsReferencesInline(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"},{"inlineData":{"mimeType":"image/png","data":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}}]}}]}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})

	res, err := b.Image[domain.ModelNanoBanana].GenerateImage(context.Background(), "gemini-nano-banana-2.1",
		modeladapter.ImageRequest{Prompt: "keep the face", AspectRatio: "1:1", Inputs: []modeladapter.Input{{Data: []byte("REF"), MimeType: "image/jpeg"}}})

	require.NoError(t, err)
	assert.Equal(t, []byte("IMG"), res.Data)
	assert.Contains(t, body, base64.StdEncoding.EncodeToString([]byte("REF")))
	assert.Contains(t, body, `"aspectRatio":"1:1"`)
}

func TestOpenAIImageBackendUsesEditsWhenReferencesAreGiven(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		assert.Equal(t, "Bearer k", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}]}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"openai": "k"})
	be := b.Image[domain.ModelGPTImage2]

	_, err := be.GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "a"})
	require.NoError(t, err)
	_, err = be.GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "b", Inputs: []modeladapter.Input{{Data: []byte("R"), MimeType: "image/png"}}})
	require.NoError(t, err)

	assert.Equal(t, []string{"/v1/images/generations", "/v1/images/edits"}, paths)
}
