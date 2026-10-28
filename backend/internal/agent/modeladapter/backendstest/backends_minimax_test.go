package backendstest_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// minimaxServer answers the MiniMax video API: the create call (recording its body), then the query call that says queued and
// running for the first two polls and then succeeded with the video at /v.mp4.
func minimaxServer(t *testing.T, submitted *map[string]any, polls *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v2/video_generation", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer mm-key", r.Header.Get("Authorization"))
		_ = json.NewDecoder(r.Body).Decode(submitted)
		_, _ = w.Write([]byte(`{"task_id":"424010985738629"}`))
	})
	mux.HandleFunc("/v2/query/video_generation/424010985738629", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer mm-key", r.Header.Get("Authorization"))
		switch polls.Add(1) {
		case 1:
			_, _ = w.Write([]byte(`{"task":{"id":"424010985738629","status":"queued"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"task":{"id":"424010985738629","status":"running"}}`))
		default:
			_, _ = w.Write([]byte(`{"task":{"id":"424010985738629","status":"succeeded","content":{"url":"` + srv.URL + `/v.mp4"},"resolution":"768P","duration":5}}`))
		}
	})
	mux.HandleFunc("/v.mp4", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"), "the result URL is the CDN's: the key does not go there")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("MMMP4"))
	})
	srv = httptest.NewServer(mux)
	return srv
}

func minimaxRequest() modeladapter.VideoRequest {
	return modeladapter.VideoRequest{Prompt: "A dancer on a drone.", DurationSeconds: 7, Resolution: "768p", AspectRatio: "9:16"}
}

func TestMiniMaxTextToVideoSendsTheTextItemAndAnExplicitRatio(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := minimaxServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})

	res, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", minimaxRequest())

	require.NoError(t, err)
	assert.Equal(t, []byte("MMMP4"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.EqualValues(t, 3, polls.Load(), "queued and running are waited for")
	assert.Equal(t, map[string]any{
		"model":      "MiniMax-H3",
		"content":    []any{map[string]any{"type": "text", "text": "A dancer on a drone."}},
		"duration":   float64(7),
		"resolution": "768P",
		"ratio":      "9:16",
	}, submitted, "text-to-video requires a ratio that is not adaptive")
}

func TestMiniMaxNamesTheResolutionAsTheAPIDoes(t *testing.T) {
	for mamName, apiName := range map[string]string{"480p": "480P", "768p": "768P", "2k": "2K"} {
		var submitted map[string]any
		var polls atomic.Int32
		srv := minimaxServer(t, &submitted, &polls)
		b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})
		req := minimaxRequest()
		req.Resolution = mamName

		_, err := b.Video[domain.ModelMiniMaxH3Max].GenerateVideo(context.Background(), "MiniMax-H3-Max", req)

		require.NoError(t, err, mamName)
		assert.Equal(t, apiName, submitted["resolution"])
		assert.Equal(t, "MiniMax-H3-Max", submitted["model"], "H3 Max takes the same request with its own model name")
		srv.Close()
	}
}

func dataURI(mime, data string) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(data))
}

func imageItem(role, mime, data string) map[string]any {
	return map[string]any{"type": "image_url", "role": role, "image_url": map[string]any{"url": dataURI(mime, data)}}
}

func TestMiniMaxFirstAndLastFrameAreImageToVideoWithoutARatio(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := minimaxServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})
	req := minimaxRequest()
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/png"}
	req.LastFrame = &modeladapter.Input{Data: []byte("LAST"), MimeType: "image/jpeg"}
	req.References = []modeladapter.Input{{Data: []byte("REF"), MimeType: "image/png"}}

	_, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "A dancer on a drone."},
		imageItem("first_frame", "image/png", "FIRST"),
		imageItem("last_frame", "image/jpeg", "LAST"),
	}, submitted["content"], "frames and reference images are exclusive: the references are left out")
	assert.NotContains(t, submitted, "ratio", "the ratio of an image-to-video task comes from the image")
}

func TestMiniMaxFirstFrameAloneIsImageToVideo(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := minimaxServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})
	req := minimaxRequest()
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/webp"}

	_, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "A dancer on a drone."},
		imageItem("first_frame", "image/webp", "FIRST"),
	}, submitted["content"])
	assert.NotContains(t, submitted, "ratio")
}

func TestMiniMaxReferenceImagesAreReferenceToVideoWithTheRatioOfTheShot(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := minimaxServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})
	req := minimaxRequest()
	req.LastFrame = &modeladapter.Input{Data: []byte("LAST"), MimeType: "image/png"} // a first frame is missing: the references win
	req.References = []modeladapter.Input{{Data: []byte("A"), MimeType: "image/png"}, {Data: []byte("B"), MimeType: "image/jpeg"}}

	_, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "A dancer on a drone."},
		imageItem("reference_image", "image/png", "A"),
		imageItem("reference_image", "image/jpeg", "B"),
	}, submitted["content"], "reference-to-video never carries first_frame / last_frame")
	assert.Equal(t, "9:16", submitted["ratio"])
}

func TestMiniMaxLastFrameAloneIsImageToVideo(t *testing.T) {
	var submitted map[string]any
	var polls atomic.Int32
	srv := minimaxServer(t, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})
	req := minimaxRequest()
	req.LastFrame = &modeladapter.Input{Data: []byte("LAST"), MimeType: "image/png"}

	_, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", req)

	require.NoError(t, err)
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "A dancer on a drone."},
		imageItem("last_frame", "image/png", "LAST"),
	}, submitted["content"])
	assert.NotContains(t, submitted, "ratio")
}

func TestMiniMaxFailedOrCancelledTaskIsARejectionNotARetry(t *testing.T) {
	for _, status := range []string{"failed", "cancelled"} {
		var creates atomic.Int32
		mux := http.NewServeMux()
		mux.HandleFunc("/v2/video_generation", func(w http.ResponseWriter, _ *http.Request) {
			creates.Add(1)
			_, _ = w.Write([]byte(`{"task_id":"t1"}`))
		})
		mux.HandleFunc("/v2/query/video_generation/t1", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"task":{"id":"t1","status":"` + status + `","error":{"code":"1026","message":"video description contains sensitive content"}}}`))
		})
		srv := httptest.NewServer(mux)
		b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})

		_, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", minimaxRequest())

		require.Error(t, err, status)
		assert.True(t, errors.Is(err, modeladapter.ErrRejected), status)
		assert.Contains(t, err.Error(), "sensitive content")
		assert.EqualValues(t, 1, creates.Load(), "the failed job is not submitted again (billing)")
		srv.Close()
	}
}

func TestMiniMaxHTTPErrorsOfTheCreateCallAreClassifiedByStatus(t *testing.T) {
	for status, check := range map[int]func(error) bool{
		http.StatusPaymentRequired:       func(err error) bool { return errors.Is(err, modeladapter.ErrRejected) },
		http.StatusUnprocessableEntity:   func(err error) bool { return errors.Is(err, modeladapter.ErrRejected) },
		http.StatusTooManyRequests:       modeladapter.RateLimited,
		http.StatusInternalServerError:   modeladapter.IsUnavailable,
		http.StatusUnauthorized:          func(err error) bool { return errors.Is(err, modeladapter.ErrRejected) },
		http.StatusRequestEntityTooLarge: func(err error) bool { return errors.Is(err, modeladapter.ErrRejected) },
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"x","message":"nope (1002)","http_code":"x"}}`))
		}))
		b := backendsFor(t, srv, memKeys{"minimax": "mm-key"})

		_, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", minimaxRequest())

		require.Error(t, err, status)
		assert.True(t, check(err), "HTTP %d: %v", status, err)
		srv.Close()
	}
}

func TestMiniMaxWithoutAKeyDoesNotCallTheAPI(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"kling4": "another vendor's key"})

	for _, m := range []domain.ModelName{domain.ModelMiniMaxH3, domain.ModelMiniMaxH3Max} {
		_, err := b.Video[m].GenerateVideo(context.Background(), "x", minimaxRequest())
		require.ErrorIs(t, err, modeladapter.ErrNoAPIKey, m)
	}
	assert.Zero(t, hits.Load())
}

func TestMiniMaxResultURLMustBeAnHTTPSURLOfAPublicHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/video_generation", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"task_id":"t1"}`)) })
	mux.HandleFunc("/v2/query/video_generation/t1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"task":{"status":"succeeded","content":{"url":"http://127.0.0.1/v.mp4"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var sent []string
	b := strictBackendsFor(t, srv, memKeys{"minimax": "mm-key"}, &sent)

	_, err := b.Video[domain.ModelMiniMaxH3].GenerateVideo(context.Background(), "MiniMax-H3", minimaxRequest())

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Contains(t, pe.Message, "result URL")
	for _, u := range sent {
		assert.True(t, strings.HasPrefix(u, srv.URL), "no request may go to %s", u)
	}
}
