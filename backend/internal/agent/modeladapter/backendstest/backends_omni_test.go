package backendstest_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

const omniAPIModel = "gemini-omni-1.1-flash"

func omniRequest() modeladapter.VideoRequest {
	return modeladapter.VideoRequest{Prompt: "A cat on a windowsill.", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"}
}

func omniPNG(s string) *modeladapter.Input {
	return &modeladapter.Input{Data: []byte(s), MimeType: "image/png"}
}

// omniServer answers the Interactions API with a video delivered as a file that is PROCESSING for the first two polls.
// submitted is the decoded body of the POST; downloadKey the key query of the download.
func omniServer(t *testing.T, submitted *map[string]any, downloadKey *atomic.Value, polls *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v1beta/interactions", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "k", r.Header.Get("x-goog-api-key"))
		_ = json.NewDecoder(r.Body).Decode(submitted)
		_, _ = w.Write([]byte(`{"id":"v1_1","status":"completed","steps":[{"type":"user_input"},{"type":"thought","content":[{"type":"thought","text":"t"}]},` +
			`{"type":"model_output","content":[{"type":"video","mime_type":"video/mp4","uri":"` + srv.URL + `/v1beta/files/f1:download?alt=media"}]}]}`))
	})
	mux.HandleFunc("/v1beta/files/f1", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "k", r.Header.Get("x-goog-api-key"))
		if polls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"name":"files/f1","state":"PROCESSING"}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"files/f1","state":"ACTIVE"}`))
	})
	mux.HandleFunc("/v1beta/files/f1:download", func(w http.ResponseWriter, r *http.Request) {
		downloadKey.Store(r.URL.Query().Get("key"))
		assert.Equal(t, "media", r.URL.Query().Get("alt"))
		assert.Empty(t, r.Header.Get("x-goog-api-key"), "the key is in the query: a redirect would forward a header to another host")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("OMNIMP4"))
	})
	srv = httptest.NewServer(mux)
	return srv
}

func TestGeminiOmniBackendAsksForTheVideoPollsTheFileAndDownloadsIt(t *testing.T) {
	var submitted map[string]any
	var downloadKey atomic.Value
	var polls atomic.Int32
	srv := omniServer(t, &submitted, &downloadKey, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})

	res, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, omniRequest())

	require.NoError(t, err)
	assert.Equal(t, []byte("OMNIMP4"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.EqualValues(t, 3, polls.Load(), "the file is polled until it is ACTIVE")
	assert.Equal(t, "k", downloadKey.Load())
	assert.Equal(t, omniAPIModel, submitted["model"])
	assert.Equal(t, map[string]any{"type": "video", "aspect_ratio": "16:9", "resolution": "720p", "delivery": "uri"}, submitted["response_format"])
	assert.Equal(t, map[string]any{"video_config": map[string]any{"task": "text_to_video"}}, submitted["generation_config"])
	assert.Equal(t, []any{map[string]any{"type": "text", "text": "A cat on a windowsill."}}, submitted["input"], "no images for a text-to-video request")
}

func TestGeminiOmniBackendSendsTheFirstFrameBeforeThePromptWithItsTag(t *testing.T) {
	var submitted map[string]any
	var downloadKey atomic.Value
	var polls atomic.Int32
	srv := omniServer(t, &submitted, &downloadKey, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})
	req := omniRequest()
	req.AspectRatio = "9:16"
	req.FirstFrame = omniPNG("FIRST")

	_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"type": "image", "mime_type": "image/png", "data": base64.StdEncoding.EncodeToString([]byte("FIRST"))},
		map[string]any{"type": "text", "text": "<FIRST_FRAME> A cat on a windowsill."},
	}, submitted["input"])
	assert.Equal(t, map[string]any{"video_config": map[string]any{"task": "image_to_video"}}, submitted["generation_config"])
	assert.Equal(t, "9:16", submitted["response_format"].(map[string]any)["aspect_ratio"])
}

func TestGeminiOmniBackendInterpolatesBetweenTheFirstAndTheLastFrame(t *testing.T) {
	var submitted map[string]any
	var downloadKey atomic.Value
	var polls atomic.Int32
	srv := omniServer(t, &submitted, &downloadKey, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})
	req := omniRequest()
	req.FirstFrame, req.LastFrame = omniPNG("FIRST"), omniPNG("LAST")

	_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, req)

	require.NoError(t, err)
	input := submitted["input"].([]any)
	require.Len(t, input, 3)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("FIRST")), input[0].(map[string]any)["data"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("LAST")), input[1].(map[string]any)["data"])
	assert.Equal(t, "<FIRST_FRAME> <LAST_FRAME> A cat on a windowsill.", input[2].(map[string]any)["text"])
	assert.NotContains(t, submitted, "generation_config", "the two frames have no task of their own: the tags say it")
}

func TestGeminiOmniBackendDeclaresReferenceImagesInThePrompt(t *testing.T) {
	var submitted map[string]any
	var downloadKey atomic.Value
	var polls atomic.Int32
	srv := omniServer(t, &submitted, &downloadKey, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})
	req := omniRequest()
	req.References = []modeladapter.Input{*omniPNG("A"), *omniPNG("B")}

	_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, req)

	require.NoError(t, err)
	input := submitted["input"].([]any)
	require.Len(t, input, 3)
	text := input[2].(map[string]any)["text"].(string)
	assert.Contains(t, text, "[# References <IMAGE_REF_0>@Image1 <IMAGE_REF_1>@Image2] A cat on a windowsill.")
	assert.Contains(t, text, "should not be used as literal initial frames")
	assert.Equal(t, map[string]any{"video_config": map[string]any{"task": "reference_to_video"}}, submitted["generation_config"])
}

func TestGeminiOmniBackendReadsAnInlineVideo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"video","mime_type":"video/mp4","data":"` +
			base64.StdEncoding.EncodeToString([]byte("INLINE")) + `"}]}]}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})

	res, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, omniRequest())

	require.NoError(t, err)
	assert.Equal(t, []byte("INLINE"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType)
}

func TestGeminiOmniBackendWaitsForAnInteractionThatRunsInTheBackground(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/interactions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"v1_bg","status":"in_progress"}`))
	})
	mux.HandleFunc("/v1beta/interactions/v1_bg", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "k", r.Header.Get("x-goog-api-key"))
		if polls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"id":"v1_bg","status":"in_progress"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"v1_bg","status":"completed","steps":[{"type":"model_output","content":[{"type":"video","data":"` +
			base64.StdEncoding.EncodeToString([]byte("LATE")) + `"}]}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})

	res, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, omniRequest())

	require.NoError(t, err)
	assert.Equal(t, []byte("LATE"), fileBytes(t, res.File))
	assert.EqualValues(t, 3, polls.Load())
	assert.Equal(t, "video/mp4", res.MimeType, "a missing mime type falls back to mp4")
}

func TestGeminiOmniBackendReportsAFilteredPromptAndAFailedJob(t *testing.T) {
	for name, tc := range map[string]struct {
		answer string
		reject bool
	}{
		"filtered": {answer: `{"status":"completed","steps":[{"type":"user_input"}]}`},
		"failed":   {answer: `{"status":"failed"}`, reject: true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.answer)) }))
			defer srv.Close()
			b := backendsFor(t, srv, memKeys{"google": "k"})

			_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, omniRequest())

			require.Error(t, err)
			assert.Equal(t, tc.reject, errors.Is(err, modeladapter.ErrRejected))
			assert.False(t, modeladapter.IsUnavailable(err), "the vendor is up: do not count it for the circuit breaker")
		})
	}
}

func TestGeminiOmniBackendRefusesRatiosItCannotMakeWithoutAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})
	req := omniRequest()
	req.AspectRatio = "1:1"

	_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, req)

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, http.StatusUnprocessableEntity, pe.Status, "422: the Adapter routes to the alternative model")
	assert.Zero(t, hits.Load())
}

func TestGeminiOmniBackendNeverSendsTheKeyToAnotherHost(t *testing.T) {
	var stolen atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { stolen.Add(1) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"completed","steps":[{"type":"model_output","content":[{"type":"video","uri":"` + other.URL + `/v1beta/files/f1:download?alt=media"}]}]}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "k"})

	_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, omniRequest())

	require.Error(t, err)
	assert.Zero(t, stolen.Load())
}

func TestGeminiOmniBackendWithoutAKeyFailsBeforeAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{})

	_, err := b.Video[domain.ModelGeminiOmniFlash].GenerateVideo(context.Background(), omniAPIModel, omniRequest())

	require.ErrorIs(t, err, modeladapter.ErrNoAPIKey)
	assert.Zero(t, hits.Load())
}
