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

// lumaServer answers the Luma Agents API: the create call (recording its body), then the generation that is queued and
// processing for the first two polls and then completed with the video at /v.mp4.
func lumaServer(t *testing.T, submitted *map[string]any, polls *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("POST /v1/generations", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer luma-key", r.Header.Get("Authorization"))
		_ = json.NewDecoder(r.Body).Decode(submitted)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gen-1","type":"video","state":"queued","model":"ray-3.2","output":[]}`))
	})
	mux.HandleFunc("GET /v1/generations/gen-1", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer luma-key", r.Header.Get("Authorization"))
		switch polls.Add(1) {
		case 1:
			_, _ = w.Write([]byte(`{"id":"gen-1","state":"queued","output":[]}`))
		case 2:
			_, _ = w.Write([]byte(`{"id":"gen-1","state":"processing","output":[]}`))
		default:
			_, _ = w.Write([]byte(`{"id":"gen-1","state":"completed","output":[{"type":"video","url":"` + srv.URL + `/v.mp4"}]}`))
		}
	})
	mux.HandleFunc("/v.mp4", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("LUMAMP4"))
	})
	srv = httptest.NewServer(mux)
	return srv
}

func lumaRequest() modeladapter.VideoRequest {
	return modeladapter.VideoRequest{Prompt: "A misty greenhouse at sunrise.", DurationSeconds: 10, Resolution: "720p", AspectRatio: "9:16"}
}

func TestLumaTextToVideoSendsTypeVideoWithTheDurationAsAString(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := lumaServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"luma": "luma-key"})

	res, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", lumaRequest())

	require.NoError(t, err)
	assert.Equal(t, []byte("LUMAMP4"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.EqualValues(t, 3, polls.Load())
	assert.Equal(t, map[string]any{
		"model": "ray-3.2", "type": "video", "prompt": "A misty greenhouse at sunrise.", "aspect_ratio": "9:16",
		"video": map[string]any{"resolution": "720p", "duration": "10s"},
	}, submitted)
}

func TestLumaFramesAreStartAndEndFramesAndTheRatioComesFromThem(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := lumaServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"luma": "luma-key"})
	req := lumaRequest()
	req.DurationSeconds = 5
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/png"}
	req.LastFrame = &modeladapter.Input{Data: []byte("LAST"), MimeType: "image/jpeg"}

	_, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", req)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"model": "ray-3.2", "type": "video", "prompt": "A misty greenhouse at sunrise.",
		"video": map[string]any{
			"resolution": "720p", "duration": "5s",
			"start_frame": map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("FIRST")), "media_type": "image/png"},
			"end_frame":   map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("LAST")), "media_type": "image/jpeg"},
		},
	}, submitted, "no aspect_ratio with frames")
}

func TestLumaFirstFrameAloneSendsOnlyTheStartFrame(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := lumaServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"luma": "luma-key"})
	req := lumaRequest()
	req.DurationSeconds = 5
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/png"}

	_, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", req)

	require.NoError(t, err)
	video := submitted["video"].(map[string]any)
	assert.Contains(t, video, "start_frame")
	assert.NotContains(t, video, "end_frame")
}

func TestLumaContentModerationIsARejectionAndNotRetried(t *testing.T) {
	var creates atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/generations", func(w http.ResponseWriter, _ *http.Request) {
		creates.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gen-1","state":"queued"}`))
	})
	mux.HandleFunc("GET /v1/generations/gen-1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gen-1","state":"failed","failure_code":"content_moderated","failure_reason":"prompt rejected"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"luma": "luma-key"})

	_, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", lumaRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, modeladapter.ErrRejected))
	assert.Contains(t, err.Error(), "prompt rejected")
	assert.EqualValues(t, 1, creates.Load())
}

func TestLumaOwnFailureIsUnavailableButTheJobIsNotSubmittedAgain(t *testing.T) {
	var creates atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/generations", func(w http.ResponseWriter, _ *http.Request) {
		creates.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gen-1","state":"queued"}`))
	})
	mux.HandleFunc("GET /v1/generations/gen-1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gen-1","state":"failed","failure_code":"generation_failed","failure_reason":"internal error"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"luma": "luma-key"})

	_, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", lumaRequest())

	require.Error(t, err)
	assert.False(t, errors.Is(err, modeladapter.ErrRejected))
	assert.True(t, modeladapter.IsUnavailable(err))
	assert.EqualValues(t, 1, creates.Load(), "the failed generation is refunded but never asked again")
}

func TestLumaSynchronousErrorsAreClassified(t *testing.T) {
	for status, check := range map[int]func(error){
		http.StatusPaymentRequired: func(err error) { assert.True(t, errors.Is(err, modeladapter.ErrRejected)) },
		http.StatusTooManyRequests: func(err error) { assert.True(t, modeladapter.RateLimited(err)) },
		http.StatusBadGateway:      func(err error) { assert.True(t, modeladapter.IsUnavailable(err)) },
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"detail":"nope"}`))
		}))
		b := backendsFor(t, srv, memKeys{"luma": "luma-key"})

		_, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", lumaRequest())

		require.Error(t, err, status)
		check(err)
		srv.Close()
	}
}

func TestLumaWithoutAKeyDoesNotCallTheAPI(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{})

	_, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", lumaRequest())

	require.ErrorIs(t, err, modeladapter.ErrNoAPIKey)
	assert.Zero(t, hits.Load())
}

func TestLumaRefusesAResultURLThatIsNotAPublicHTTPSURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/generations", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gen-1","state":"queued"}`))
	})
	mux.HandleFunc("GET /v1/generations/gen-1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"gen-1","state":"completed","output":[{"type":"video","url":"http://169.254.169.254/latest/meta-data"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var sent []string
	b := strictBackendsFor(t, srv, memKeys{"luma": "luma-key"}, &sent)

	_, err := b.Video[domain.ModelLumaRay32].GenerateVideo(context.Background(), "ray-3.2", lumaRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, modeladapter.ErrAfterSubmit))
	for _, u := range sent {
		assert.NotContains(t, u, "169.254.169.254", "the result URL is never fetched")
	}
}
