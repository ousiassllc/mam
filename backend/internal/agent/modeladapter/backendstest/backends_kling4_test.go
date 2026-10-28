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

// kling4Server answers the new Kling API: the create call at path (recording its body), then /tasks that is processing for the
// first two polls and then succeeded with the video at /v.mp4.
func kling4Server(t *testing.T, path string, submitted *map[string]any, polls *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer api-key", r.Header.Get("Authorization"), "the new API takes the API key itself, not a JWT")
		_ = json.NewDecoder(r.Body).Decode(submitted)
		_, _ = w.Write([]byte(`{"code":0,"message":"","data":{"id":"t1","status":"submitted"}}`))
	})
	mux.HandleFunc("/tasks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer api-key", r.Header.Get("Authorization"))
		assert.Equal(t, "t1", r.URL.Query().Get("task_ids"))
		if polls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"code":0,"data":[{"id":"t1","status":"processing"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":[{"id":"t1","status":"succeeded","outputs":[{"type":"image","url":"` + srv.URL + `/x.png"},{"type":"video","url":"` + srv.URL + `/v.mp4"}]}]}`))
	})
	mux.HandleFunc("/v.mp4", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("K4MP4"))
	})
	srv = httptest.NewServer(mux)
	return srv
}

func kling4Request() modeladapter.VideoRequest {
	return modeladapter.VideoRequest{Prompt: "A girl on a train.", DurationSeconds: 12, Resolution: "1080p", AspectRatio: "9:16"}
}

func TestKling4BackendTextToVideoUsesTheVersionedPathAndTheSettingsObject(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := kling4Server(t, "/text-to-video/kling-4.0", &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling4": "api-key"})

	res, err := b.Video[domain.ModelKling4].GenerateVideo(context.Background(), "kling-4.0", kling4Request())

	require.NoError(t, err)
	assert.Equal(t, []byte("K4MP4"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.EqualValues(t, 3, polls.Load())
	assert.Equal(t, map[string]any{
		"prompt":   "A girl on a train.",
		"settings": map[string]any{"duration": float64(12), "resolution": "1080p", "aspect_ratio": "9:16"},
	}, submitted)
}

func TestKling4BackendImageToVideoSendsTheFramesAsContents(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := kling4Server(t, "/image-to-video/kling-4.0", &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling4": "api-key"})
	req := kling4Request()
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/png"}
	req.LastFrame = &modeladapter.Input{Data: []byte("LAST"), MimeType: "image/png"}

	_, err := b.Video[domain.ModelKling4].GenerateVideo(context.Background(), "kling-4.0", req)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"contents": []any{
			map[string]any{"type": "prompt", "text": "A girl on a train."},
			map[string]any{"type": "first_frame", "url": base64.StdEncoding.EncodeToString([]byte("FIRST"))},
			map[string]any{"type": "last_frame", "url": base64.StdEncoding.EncodeToString([]byte("LAST"))},
		},
		"settings": map[string]any{"duration": float64(12), "resolution": "1080p"},
	}, submitted, "the output keeps the aspect ratio of the first frame: no aspect_ratio")
}

func TestKling4BackendImageToVideoWithoutALastFrameSendsOnlyTheFirst(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := kling4Server(t, "/image-to-video/kling-4.0", &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling4": "api-key"})
	req := kling4Request()
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/png"}

	_, err := b.Video[domain.ModelKling4].GenerateVideo(context.Background(), "kling-4.0", req)

	require.NoError(t, err)
	assert.Len(t, submitted["contents"], 2)
}

func TestKling4BackendFailedTaskIsARejectionNotARetry(t *testing.T) {
	var creates atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/text-to-video/kling-4.0", func(w http.ResponseWriter, _ *http.Request) {
		creates.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":"t1"}}`))
	})
	mux.HandleFunc("/tasks", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":[{"id":"t1","status":"failed","message":"content risk control"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling4": "api-key"})

	_, err := b.Video[domain.ModelKling4].GenerateVideo(context.Background(), "kling-4.0", kling4Request())

	require.Error(t, err)
	assert.True(t, errors.Is(err, modeladapter.ErrRejected))
	assert.Contains(t, err.Error(), "content risk control")
	assert.EqualValues(t, 1, creates.Load())
}

func TestKling4BackendMapsKlingCodesLikeKling3(t *testing.T) {
	for code, wantStatus := range map[int]int{1301: http.StatusUnprocessableEntity, 1302: http.StatusTooManyRequests, 5000: http.StatusBadGateway} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			body, _ := json.Marshal(map[string]any{"code": code, "message": "nope"})
			_, _ = w.Write(body)
		}))
		b := backendsFor(t, srv, memKeys{"kling4": "api-key"})

		_, err := b.Video[domain.ModelKling4].GenerateVideo(context.Background(), "kling-4.0", kling4Request())

		var pe *modeladapter.ProviderError
		require.ErrorAs(t, err, &pe)
		assert.Equal(t, wantStatus, pe.Status, "code %d", code)
		srv.Close()
	}
}

func TestKling4BackendWithoutAKeyFailsBeforeAnyRequestAndDoesNotUseTheKling3Key(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling": "ACCESS:SECRET"})

	_, err := b.Video[domain.ModelKling4].GenerateVideo(context.Background(), "kling-4.0", kling4Request())

	require.ErrorIs(t, err, modeladapter.ErrNoAPIKey)
	assert.Zero(t, hits.Load())
}

func TestKling3StillUsesTheOldPathAndTheJWT(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"code":1301,"message":"stop here"}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling": "ACCESS:SECRET"})

	_, err := b.Video[domain.ModelKling3].GenerateVideo(context.Background(), "kling-v3", modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

	require.Error(t, err)
	assert.Equal(t, "/v1/videos/text2video", path)
	assert.Regexp(t, `^Bearer [\w-]+\.[\w-]+\.[\w-]+$`, auth, "Kling 3.0 keeps its JWT")
}
