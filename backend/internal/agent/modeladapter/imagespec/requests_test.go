package imagespec_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Package imagespec pins the default image models, routes and prices and the shape of their requests against
// the vendors' published specs (spec/architecture/model-adapters.md §9). It is a package of its own because
// package modeladapter is over its line limit.

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

// The request shapes below are pinned against the vendors' published specs: OpenAI's openapi.yaml and image
// generation guide (gpt-image-2: no response_format, no input_fidelity, b64_json always returned) and Google's
// Gemini API reference (models/*:generateContent, generationConfig.imageConfig).

func TestOpenAIGenerationsRequestMatchesTheGPTImage2Spec(t *testing.T) {
	for aspect, size := range map[string]string{"1:1": "1024x1024", "16:9": "1536x1024", "9:16": "1024x1536"} {
		t.Run(aspect, func(t *testing.T) {
			var gotPath, gotAuth, gotContentType string
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotAuth, gotContentType = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}]}`))
			}))
			defer srv.Close()
			b := backendsFor(t, srv, memKeys{"openai": "sk-test"})

			res, err := b.Image[domain.ModelGPTImage2].GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{Prompt: "a bottle", AspectRatio: aspect})

			require.NoError(t, err)
			assert.Equal(t, []byte("IMG"), res.Data)
			assert.Equal(t, "/v1/images/generations", gotPath)
			assert.Equal(t, "Bearer sk-test", gotAuth)
			assert.Equal(t, "application/json", gotContentType)
			assert.Equal(t, map[string]any{"model": "gpt-image-2", "prompt": "a bottle", "size": size, "n": float64(1)}, body,
				"exactly model, prompt, size, n: no response_format, input_fidelity, quality or background")
		})
	}
}

func TestOpenAIEditsRequestMatchesTheGPTImage2Spec(t *testing.T) {
	var gotPath, gotAuth string
	fields := map[string][]string{}
	files := map[string][][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !assert.NoError(t, err) || !assert.Equal(t, "multipart/form-data", mediaType) {
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if !assert.NoError(t, err) {
				return
			}
			data, _ := io.ReadAll(part)
			if part.FileName() != "" {
				files[part.FormName()] = append(files[part.FormName()], data)
			} else {
				fields[part.FormName()] = append(fields[part.FormName()], string(data))
			}
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("EDITED")) + `"}]}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"openai": "sk-test"})

	res, err := b.Image[domain.ModelGPTImage2].GenerateImage(context.Background(), "gpt-image-2", modeladapter.ImageRequest{
		Prompt: "combine them", AspectRatio: "9:16",
		Inputs: []modeladapter.Input{{Data: []byte("ONE"), MimeType: "image/png"}, {Data: []byte("TWO"), MimeType: "image/jpeg"}},
	})

	require.NoError(t, err)
	assert.Equal(t, []byte("EDITED"), res.Data, "data[0].b64_json is decoded")
	assert.Equal(t, "/v1/images/edits", gotPath)
	assert.Equal(t, "Bearer sk-test", gotAuth)
	assert.Equal(t, map[string][]string{"model": {"gpt-image-2"}, "prompt": {"combine them"}, "size": {"1024x1536"}}, fields,
		"no input_fidelity and no response_format field")
	assert.Equal(t, map[string][][]byte{"image[]": {[]byte("ONE"), []byte("TWO")}}, files, "one image[] file part per input, in order")
}

func TestGeminiImageModelsCallGenerateContentWithTheImageConfig(t *testing.T) {
	for _, tt := range []struct {
		model  domain.ModelName
		apiID  string
		aspect string
	}{
		{domain.ModelNanoBanana, "gemini-nano-banana-2.1", "16:9"},
		{domain.ModelNanoBananaLite, "gemini-3.1-flash-lite-image", "9:16"},
	} {
		t.Run(string(tt.model), func(t *testing.T) {
			var gotPath, gotKey string
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotKey = r.URL.Path, r.Header.Get("x-goog-api-key")
				assert.Equal(t, http.MethodPost, r.Method)
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}}]}}]}`))
			}))
			defer srv.Close()
			cfg := modeladapter.DefaultConfig()
			b := backendsFor(t, srv, memKeys{"google": "g-key"})

			res, err := b.Image[tt.model].GenerateImage(context.Background(), cfg.Models[tt.model].Model, modeladapter.ImageRequest{Prompt: "a bottle", AspectRatio: tt.aspect})

			require.NoError(t, err)
			assert.Equal(t, []byte("IMG"), res.Data)
			assert.Equal(t, "/v1beta/models/"+tt.apiID+":generateContent", gotPath)
			assert.Equal(t, "g-key", gotKey)
			gen, ok := body["generationConfig"].(map[string]any)
			require.True(t, ok, "generationConfig is sent")
			assert.Equal(t, []any{"IMAGE"}, gen["responseModalities"])
			assert.Equal(t, map[string]any{"aspectRatio": tt.aspect}, gen["imageConfig"],
				"no imageSize: unset is the model's default 1K, the size CostPerImage prices")
		})
	}
}
