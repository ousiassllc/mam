package modeladapter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestFluxBackendPollsUntilReadyThenDownloads(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v1/flux-2-pro", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "k", r.Header.Get("x-key"))
		_, _ = w.Write([]byte(`{"id":"j1","polling_url":"` + srv.URL + `/poll"}`))
	})
	mux.HandleFunc("/poll", func(w http.ResponseWriter, _ *http.Request) {
		if polls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"status":"Pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + srv.URL + `/file.jpg"}}`))
	})
	mux.HandleFunc("/file.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEG"))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"flux": "k"})

	res, err := b.Image[domain.ModelFlux2].GenerateImage(context.Background(), "flux-2-pro", modeladapter.ImageRequest{Prompt: "p", AspectRatio: "16:9"})

	require.NoError(t, err)
	assert.Equal(t, []byte("JPEG"), res.Data)
	assert.Equal(t, "image/jpeg", res.MimeType)
	assert.EqualValues(t, 3, polls.Load())
}

func TestSeedanceBackendSubmitsPollsAndDownloads(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer k", r.Header.Get("Authorization"))
		_ = json.NewDecoder(r.Body).Decode(&submitted)
		_, _ = w.Write([]byte(`{"id":"task1"}`))
	})
	mux.HandleFunc("/api/v3/contents/generations/tasks/task1", func(w http.ResponseWriter, _ *http.Request) {
		if polls.Add(1) < 2 {
			_, _ = w.Write([]byte(`{"status":"running"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"` + srv.URL + `/v.mp4"}}`))
	})
	mux.HandleFunc("/v.mp4", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("MP4"))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	res, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "seedance-2-0", modeladapter.VideoRequest{
		Prompt: "run", DurationSeconds: 4, Resolution: "720p", AspectRatio: "16:9",
		References: []modeladapter.Input{{Data: []byte("R"), MimeType: "image/png"}},
	})

	require.NoError(t, err)
	assert.Equal(t, []byte("MP4"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.Equal(t, "seedance-2-0", submitted["model"])
	content := submitted["content"].([]any)
	assert.Contains(t, content[0].(map[string]any)["text"], "--resolution 720p --duration 4 --ratio 16:9")
	assert.Equal(t, "reference_image", content[1].(map[string]any)["role"])
}

func TestSeedanceNeverSendsTheFirstFrameAndReferenceImagesTogether(t *testing.T) {
	var submitted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&submitted)
		_, _ = w.Write([]byte(`{"id":"task1"}`))
	})
	mux.HandleFunc("/api/v3/contents/generations/tasks/task1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"ftp://127.0.0.1/missing.mp4"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	_, _ = b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "seedance-2-0", modeladapter.VideoRequest{
		Prompt: "run", DurationSeconds: 4, Resolution: "720p", AspectRatio: "16:9",
		FirstFrame: &modeladapter.Input{Data: []byte("F"), MimeType: "image/png"},
		LastFrame:  &modeladapter.Input{Data: []byte("L"), MimeType: "image/png"},
		References: []modeladapter.Input{{Data: []byte("R"), MimeType: "image/png"}},
	})

	content := submitted["content"].([]any)
	roles := make([]string, len(content)-1)
	for i, item := range content[1:] {
		roles[i] = item.(map[string]any)["role"].(string)
	}
	assert.Equal(t, []string{"first_frame", "last_frame"}, roles, "the API treats frames and reference images as exclusive scenarios")
}

func TestSeedanceFailedJobIsARejectionNotARetry(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":"t"}`)) })
	mux.HandleFunc("/api/v3/contents/generations/tasks/t", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed","error":{"message":"content policy"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	_, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 4, Resolution: "720p", AspectRatio: "16:9"})

	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Contains(t, err.Error(), "content policy")
}

func TestKlingBackendSignsAJWTAndUsesImage2VideoWithFrames(t *testing.T) {
	var auth string
	var submitted map[string]any
	var polled atomic.Bool
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v1/videos/image2video", func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&submitted)
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_id":"k1","task_status":"submitted"}}`))
	})
	mux.HandleFunc("/v1/videos/image2video/k1", func(w http.ResponseWriter, _ *http.Request) {
		polled.Store(true)
		_, _ = w.Write([]byte(`{"code":0,"data":{"task_status":"succeed","task_result":{"videos":[{"url":"` + srv.URL + `/k.mp4"}]}}}`))
	})
	mux.HandleFunc("/k.mp4", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("KMP4")) })
	srv = httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling": "ACCESS:SECRET"})

	res, err := b.Video[domain.ModelKling3].GenerateVideo(context.Background(), "kling-v3", modeladapter.VideoRequest{
		Prompt: "run", NegativePrompt: "blurry", DurationSeconds: 5, Resolution: "1080p", AspectRatio: "16:9",
		FirstFrame: &modeladapter.Input{Data: []byte("F"), MimeType: "image/png"},
		LastFrame:  &modeladapter.Input{Data: []byte("L"), MimeType: "image/png"},
	})

	require.NoError(t, err)
	assert.Equal(t, []byte("KMP4"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType, "an unknown content type falls back to mp4")
	assert.True(t, strings.HasPrefix(auth, "Bearer "), auth)
	assert.Equal(t, 3, strings.Count(auth, ".")+1)
	assert.Equal(t, "pro", submitted["mode"], "1080p is Kling's pro mode")
	assert.Equal(t, "5", submitted["duration"])
	assert.NotEmpty(t, submitted["image_tail"])
	assert.True(t, polled.Load())
}

func TestKlingRateLimitCodeIsReportedAs429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":1303,"message":"parallel task over resource pack limit"}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling": "A:S"})

	_, err := b.Video[domain.ModelKling3].GenerateVideo(context.Background(), "kling-v3", modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

	assert.True(t, modeladapter.RateLimited(err))
}
