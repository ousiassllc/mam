package openaicompat_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/openaicompat"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func backendFor(t *testing.T, url string) (openaicompat.Backend, *credentialtest.Memory) {
	t.Helper()
	keys := credentialtest.New()
	c, err := localhttp.New("local", localcfg.Endpoint{Kind: localcfg.KindOpenAICompat, BaseURL: url}, keys, t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 1
	return openaicompat.Backend{Client: c, CostPerImage: 0.02}, keys
}

func TestGenerationsWithB64JSON(t *testing.T) {
	var auth, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	defer srv.Close()
	b, keys := backendFor(t, srv.URL)
	require.NoError(t, keys.Set("endpoint:local", "k-123"))

	res, err := b.GenerateImage(context.Background(), "flux-dev", modeladapter.ImageRequest{Prompt: "a fox", AspectRatio: "9:16"})
	require.NoError(t, err)

	assert.Equal(t, "/v1/images/generations", path)
	assert.Equal(t, "Bearer k-123", auth)
	assert.Equal(t, "flux-dev", body["model"])
	assert.Equal(t, "a fox", body["prompt"])
	assert.Equal(t, "1024x1536", body["size"])
	assert.Equal(t, png, res.Data)
	assert.Equal(t, "image/png", res.MimeType)
	require.NotNil(t, res.ActualCost)
	assert.InDelta(t, 0.02, *res.ActualCost, 1e-9)
}

func TestTheAPIKeyIsOptional(t *testing.T) {
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	defer srv.Close()
	b, _ := backendFor(t, srv.URL)
	assert.True(t, b.Configured())
	_, err := b.GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, "", gotAuth.Load())
}

func TestEditsWithReferenceImages(t *testing.T) {
	var path string
	var fields = map[string]string{}
	var files []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(p)
			if p.FileName() != "" {
				files = append(files, p.FormName()+":"+p.FileName())
			} else {
				fields[p.FormName()] = string(data)
			}
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	defer srv.Close()
	b, _ := backendFor(t, srv.URL)

	_, err := b.GenerateImage(context.Background(), "m", modeladapter.ImageRequest{
		Prompt: "p", AspectRatio: "1:1", Inputs: []modeladapter.Input{{Data: png, MimeType: "image/png"}, {Data: []byte("j"), MimeType: "image/jpeg"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "/v1/images/edits", path)
	assert.Equal(t, map[string]string{"model": "m", "prompt": "p", "size": "1024x1024"}, fields)
	assert.Equal(t, []string{"image[]:input0.png", "image[]:input1.jpg"}, files)
}

// The answer may name the image by URL: a file under the endpoint's own origin is fetched over plain http.
func TestURLOfTheEndpointsOwnOriginIsDownloaded(t *testing.T) {
	var base string
	var fileAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/a.jpg" {
			fileAuth.Store(r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("\xff\xd8\xffJPEG"))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"url":"` + base + `/files/a.jpg"}]}`))
	}))
	defer srv.Close()
	base = srv.URL
	b, keys := backendFor(t, srv.URL)
	require.NoError(t, keys.Set("endpoint:local", "k"))

	res, err := b.GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", res.MimeType)
	assert.Equal(t, []byte("\xff\xd8\xffJPEG"), res.Data)
	assert.Equal(t, "Bearer k", fileAuth.Load(), "a file under the base URL carries the key")
}

// A URL of any other origin follows the rules of vendor result URLs: it is refused without a connection,
// and the key is never sent there.
func TestURLOfAnotherOriginIsRefusedWithoutConnecting(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	for name, target := range map[string]string{
		"another port of the same host": other.URL + "/x.png",
		"loopback https":                "https://127.0.0.1:9/x.png",
		"private":                       "https://10.1.2.3/x.png",
		"private 192.168":               "https://192.168.0.5/x.png",
		"tailnet":                       "https://100.101.102.103/x.png",
		"metadata":                      "https://169.254.169.254/latest",
		"plain http public":             "http://cdn.example.com/x.png",
		"localhost":                     "https://localhost/x.png",
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"url":"` + target + `"}]}`))
			}))
			defer srv.Close()
			b, keys := backendFor(t, srv.URL)
			require.NoError(t, keys.Set("endpoint:local", "k"))

			_, err := b.GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "p"})

			var pe *modeladapter.ProviderError
			require.ErrorAs(t, err, &pe)
			assert.False(t, modeladapter.IsUnavailable(err), "a refused URL is no outage: no retry, no breaker")
			assert.Contains(t, strings.ToLower(pe.Error()), "refus")
		})
	}
	assert.Zero(t, elsewhere.Load())
}

func TestRedirectToAnotherHostIsRefusedAndNeverCarriesTheKey(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits.Add(1) }))
	defer foreign.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/steal", http.StatusFound)
	}))
	defer srv.Close()
	b, keys := backendFor(t, srv.URL)
	require.NoError(t, keys.Set("endpoint:local", "k"))
	_, err := b.GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "p"})
	require.Error(t, err)
	assert.Zero(t, foreignHits.Load())
}

func TestFailuresAreClassifiedForTheAdapter(t *testing.T) {
	for status, check := range map[int]func(t *testing.T, err error){
		429: func(t *testing.T, err error) { assert.True(t, modeladapter.RateLimited(err)) },
		503: func(t *testing.T, err error) { assert.True(t, modeladapter.IsUnavailable(err)) },
		400: func(t *testing.T, err error) { assert.ErrorIs(t, err, modeladapter.ErrRejected) },
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		b, _ := backendFor(t, srv.URL)
		_, err := b.GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "p"})
		require.Error(t, err)
		check(t, err)
		srv.Close()
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) }))
	defer empty.Close()
	b, _ := backendFor(t, empty.URL)
	_, err := b.GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "p"})
	require.Error(t, err)
}
