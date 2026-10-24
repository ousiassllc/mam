package modeladapter_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestKlingKeyWithoutSecretIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no request expected") }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling": "onlyone"})
	_, err := b.Video[domain.ModelKling3].GenerateVideo(context.Background(), "kling-v3", modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})
	assert.ErrorIs(t, err, modeladapter.ErrNoAPIKey)
}

func TestHTTPErrorsAreClassifiedWithRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"openai": "k"})

	_, err := b.Image[domain.ModelGPTImage2].GenerateImage(context.Background(), "m", modeladapter.ImageRequest{Prompt: "p"})

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, http.StatusTooManyRequests, pe.Status)
	assert.Equal(t, 7*time.Second, pe.RetryAfter)
	assert.True(t, modeladapter.RateLimited(err))
}

func TestDownloadRefusesANonHTTPResultURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":"t"}`)) })
	mux.HandleFunc("/api/v3/contents/generations/tasks/t", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"file:///etc/passwd"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	_, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 4, Resolution: "720p", AspectRatio: "16:9"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an http(s) URL")
}

func TestRunwayBackendUsesTextToVideoWithoutFrames(t *testing.T) {
	var submitted map[string]any
	var version, auth string
	var polls atomic.Int32
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v1/text_to_video", func(w http.ResponseWriter, r *http.Request) {
		version, auth = r.Header.Get("X-Runway-Version"), r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&submitted)
		_, _ = w.Write([]byte(`{"id":"r1"}`))
	})
	mux.HandleFunc("/v1/tasks/r1", func(w http.ResponseWriter, _ *http.Request) {
		if polls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"status":"RUNNING"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"SUCCEEDED","output":["` + srv.URL + `/r.mp4"]}`))
	})
	mux.HandleFunc("/r.mp4", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("RMP4"))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"runway": "k"})

	res, err := b.Video[domain.ModelRunwayGen45].GenerateVideo(context.Background(), "gen4.5", modeladapter.VideoRequest{
		Prompt: "run", DurationSeconds: 5, Resolution: "720p", AspectRatio: "9:16"})

	require.NoError(t, err)
	assert.Equal(t, []byte("RMP4"), fileBytes(t, res.File))
	assert.Equal(t, "Bearer k", auth)
	assert.NotEmpty(t, version)
	assert.Equal(t, "gen4.5", submitted["model"])
	assert.Equal(t, "720:1280", submitted["ratio"])
	assert.EqualValues(t, 5, submitted["duration"])
	assert.Equal(t, "run", submitted["promptText"])
	assert.EqualValues(t, 3, polls.Load())
}

func TestRunwayBackendUsesImageToVideoWithTheFirstFrame(t *testing.T) {
	var submitted map[string]any
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v1/image_to_video", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&submitted)
		_, _ = w.Write([]byte(`{"id":"r2"}`))
	})
	mux.HandleFunc("/v1/tasks/r2", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCEEDED","output":["` + srv.URL + `/r.mp4"]}`))
	})
	mux.HandleFunc("/r.mp4", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("RMP4")) })
	srv = httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"runway": "k"})

	res, err := b.Video[domain.ModelRunwayGen45].GenerateVideo(context.Background(), "gen4.5", modeladapter.VideoRequest{
		Prompt: "run", DurationSeconds: 4, Resolution: "720p", AspectRatio: "1:1",
		FirstFrame: &modeladapter.Input{Data: []byte("F"), MimeType: "image/png"}})

	require.NoError(t, err)
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.Equal(t, "960:960", submitted["ratio"])
	image := submitted["promptImage"].([]any)[0].(map[string]any)
	assert.Equal(t, "first", image["position"])
	assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("F")), image["uri"])
}

func TestRunwayBackendRefusesASquareTextToVideoWithoutRequestingIt(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"runway": "k"})

	_, err := b.Video[domain.ModelRunwayGen45].GenerateVideo(context.Background(), "gen4.5", modeladapter.VideoRequest{
		Prompt: "p", DurationSeconds: 4, Resolution: "720p", AspectRatio: "1:1"})

	require.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Zero(t, hits.Load())
}

func TestRunwayFailuresSeparateModerationFromInternalErrors(t *testing.T) {
	for _, tc := range []struct {
		code     string
		rejected bool
	}{{"SAFETY.INPUT.TEXT", true}, {"INTERNAL.BAD_OUTPUT.CODE01", false}} {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/text_to_video", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":"t"}`)) })
		mux.HandleFunc("/v1/tasks/t", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"FAILED","failure":"nope","failureCode":"` + tc.code + `"}`))
		})
		srv := httptest.NewServer(mux)
		b := backendsFor(t, srv, memKeys{"runway": "k"})

		_, err := b.Video[domain.ModelRunwayGen45].GenerateVideo(context.Background(), "gen4.5", modeladapter.VideoRequest{
			Prompt: "p", DurationSeconds: 4, Resolution: "720p", AspectRatio: "16:9"})
		srv.Close()

		require.Error(t, err)
		assert.Equal(t, tc.rejected, errors.Is(err, modeladapter.ErrRejected), tc.code)
	}
}

func TestRunwayBackendWithoutAPIKeyFailsBeforeAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{})

	_, err := b.Video[domain.ModelRunwayGen45].GenerateVideo(context.Background(), "gen4.5", modeladapter.VideoRequest{Prompt: "p"})

	require.ErrorIs(t, err, modeladapter.ErrNoAPIKey)
	assert.Zero(t, hits.Load())
}

// seedanceServer serves a Seedance job whose status and file answers come from the given codes:
// 0 = the normal answer, anything else is returned as the HTTP status. The last code repeats.
func seedanceServer(t *testing.T, pollCodes, downloadCodes []int) (srv *httptest.Server, submits, polls, downloads *atomic.Int32) {
	t.Helper()
	submits, polls, downloads = new(atomic.Int32), new(atomic.Int32), new(atomic.Int32)
	code := func(codes []int, n int32) int { return codes[min(int(n)-1, len(codes)-1)] }
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, _ *http.Request) {
		submits.Add(1)
		_, _ = w.Write([]byte(`{"id":"task1"}`))
	})
	mux.HandleFunc("/api/v3/contents/generations/tasks/task1", func(w http.ResponseWriter, _ *http.Request) {
		if c := code(pollCodes, polls.Add(1)); c != 0 {
			w.WriteHeader(c)
			return
		}
		_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"` + srv.URL + `/v.mp4"}}`))
	})
	mux.HandleFunc("/v.mp4", func(w http.ResponseWriter, _ *http.Request) {
		if c := code(downloadCodes, downloads.Add(1)); c != 0 {
			w.WriteHeader(c)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("MP4"))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, submits, polls, downloads
}
