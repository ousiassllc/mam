package modeladapter_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestImagenBackendSendsPromptAndKeyAndDecodesTheImage(t *testing.T) {
	var gotKey, gotPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotPath = r.Header.Get("x-goog-api-key"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"predictions":[{"bytesBase64Encoded":"` + base64.StdEncoding.EncodeToString([]byte("PNG")) + `","mimeType":"image/png"}]}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"google": "secret"})

	res, err := b.Image[domain.ModelImagen4].GenerateImage(context.Background(), "imagen-4.0-generate-001", modeladapter.ImageRequest{Prompt: "a bottle", AspectRatio: "16:9"})

	require.NoError(t, err)
	assert.Equal(t, []byte("PNG"), res.Data)
	assert.Equal(t, "secret", gotKey)
	assert.Equal(t, "/v1beta/models/imagen-4.0-generate-001:predict", gotPath)
	assert.Equal(t, "a bottle", body["instances"].([]any)[0].(map[string]any)["prompt"])
}

func TestImageBackendWithoutAPIKeyFailsBeforeAnyRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{})

	_, err := b.Image[domain.ModelGPTImage2].GenerateImage(context.Background(), "gpt-image-1", modeladapter.ImageRequest{Prompt: "x"})

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

	res, err := b.Image[domain.ModelNanoBanana].GenerateImage(context.Background(), "gemini-2.5-flash-image",
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

	_, err := be.GenerateImage(context.Background(), "gpt-image-1", modeladapter.ImageRequest{Prompt: "a"})
	require.NoError(t, err)
	_, err = be.GenerateImage(context.Background(), "gpt-image-1", modeladapter.ImageRequest{Prompt: "b", Inputs: []modeladapter.Input{{Data: []byte("R"), MimeType: "image/png"}}})
	require.NoError(t, err)

	assert.Equal(t, []string{"/v1/images/generations", "/v1/images/edits"}, paths)
}

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
	assert.Equal(t, []byte("MP4"), res.Data)
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.Equal(t, "seedance-2-0", submitted["model"])
	content := submitted["content"].([]any)
	assert.Contains(t, content[0].(map[string]any)["text"], "--resolution 720p --duration 4 --ratio 16:9")
	assert.Equal(t, "reference_image", content[1].(map[string]any)["role"])
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
	assert.Equal(t, []byte("KMP4"), res.Data)
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
	assert.Equal(t, []byte("RMP4"), res.Data)
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

var seedanceReq = modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 4, Resolution: "720p", AspectRatio: "16:9"}

func TestSubmittedJobSurvivesATransientPollFailureWithoutBeingSubmittedAgain(t *testing.T) {
	srv, submits, polls, _ := seedanceServer(t, []int{http.StatusServiceUnavailable, 0}, []int{0})
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	res, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", seedanceReq)

	require.NoError(t, err)
	assert.Equal(t, []byte("MP4"), res.Data)
	assert.EqualValues(t, 1, submits.Load())
	assert.EqualValues(t, 2, polls.Load())
}

func TestSubmittedJobSurvivesATransientDownloadFailureWithoutBeingSubmittedAgain(t *testing.T) {
	srv, submits, _, downloads := seedanceServer(t, []int{0}, []int{http.StatusBadGateway, 0})
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	res, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", seedanceReq)

	require.NoError(t, err)
	assert.Equal(t, []byte("MP4"), res.Data)
	assert.EqualValues(t, 1, submits.Load())
	assert.EqualValues(t, 2, downloads.Load())
}

func TestPollAndDownloadRetriesAreBounded(t *testing.T) {
	srv, _, polls, _ := seedanceServer(t, []int{http.StatusServiceUnavailable}, []int{0})
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	_, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", seedanceReq)

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, http.StatusServiceUnavailable, pe.Status)
	assert.EqualValues(t, 3, polls.Load())
}

func TestAdapterDoesNotSubmitAgainWhenPollingKeepsFailing(t *testing.T) {
	srv, submits, _, _ := seedanceServer(t, []int{http.StatusServiceUnavailable}, []int{0})
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	a := newAdapter(cfg, modeladapter.NewBackends(cfg, memKeys{"seedance": "k"}, srv.Client(), time.Millisecond, modeladapter.WithLocalDownloads()))

	_, err := a.GenerateVideo(context.Background(), seedanceTarget, seedanceTarget, baseJob())

	require.Error(t, err)
	assert.EqualValues(t, 1, submits.Load(), "the vendor accepted the job: a failed status request must not submit it (and bill it) again")
}

func TestFluxJobSurvivesATransientPollAndDownloadFailure(t *testing.T) {
	var submits, polls, downloads atomic.Int32
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/v1/flux-2-pro", func(w http.ResponseWriter, _ *http.Request) {
		submits.Add(1)
		_, _ = w.Write([]byte(`{"id":"j1","polling_url":"` + srv.URL + `/poll"}`))
	})
	mux.HandleFunc("/poll", func(w http.ResponseWriter, _ *http.Request) {
		if polls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + srv.URL + `/file.jpg"}}`))
	})
	mux.HandleFunc("/file.jpg", func(w http.ResponseWriter, _ *http.Request) {
		if downloads.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEG"))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"flux": "k"})

	res, err := b.Image[domain.ModelFlux2].GenerateImage(context.Background(), "flux-2-pro", modeladapter.ImageRequest{Prompt: "p", AspectRatio: "16:9"})

	require.NoError(t, err)
	assert.Equal(t, []byte("JPEG"), res.Data)
	assert.EqualValues(t, 1, submits.Load())
}

func TestFluxDoesNotSendTheKeyToAPollingURLOfAnotherHost(t *testing.T) {
	var foreignHits atomic.Int32
	var foreignKey atomic.Value
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits.Add(1)
		foreignKey.Store(r.Header.Get("x-key"))
		_, _ = w.Write([]byte(`{"status":"Pending"}`))
	}))
	defer foreign.Close()
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"j1","polling_url":"` + foreign.URL + `/poll"}`))
	}))
	defer vendor.Close()
	b := backendsFor(t, vendor, memKeys{"flux": "secret"})

	_, err := b.Image[domain.ModelFlux2].GenerateImage(context.Background(), "flux-2-pro", modeladapter.ImageRequest{Prompt: "p"})

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Contains(t, pe.Message, "polling URL")
	assert.Zero(t, foreignHits.Load(), "the foreign host must not be contacted")
	assert.Nil(t, foreignKey.Load())
}

func TestDownloadRefusesPlainHTTPAndNonPublicAddressesWithoutConnecting(t *testing.T) {
	for name, resultURL := range map[string]string{
		"plain http":    "http://cdn.example.com/v.mp4",
		"loopback":      "https://127.0.0.1/v.mp4",
		"localhost":     "https://localhost/v.mp4",
		"metadata":      "https://169.254.169.254/latest/meta-data",
		"private":       "https://192.168.0.10/v.mp4",
		"loopback IPv6": "https://[::1]/v.mp4",
	} {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"id":"t"}`)) })
			mux.HandleFunc("/api/v3/contents/generations/tasks/t", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"` + resultURL + `"}}`))
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			var sent []string
			b := strictBackendsFor(t, srv, memKeys{"seedance": "k"}, &sent)

			_, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", seedanceReq)

			var pe *modeladapter.ProviderError
			require.ErrorAs(t, err, &pe)
			assert.Contains(t, pe.Message, "result URL")
			for _, u := range sent {
				assert.True(t, strings.HasPrefix(u, srv.URL), "no request may go to %s", u)
			}
		})
	}
}
