package backendstest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// alibabaServer answers the Model Studio video-synthesis API: the create call (recording its headers and body), then the task
// that is PENDING and RUNNING for the first two polls and then SUCCEEDED with the video at /v.mp4.
func alibabaServer(t *testing.T, header *http.Header, submitted *map[string]any, polls *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/api/v1/services/aigc/video-generation/video-synthesis", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		*header = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(submitted)
		_, _ = w.Write([]byte(`{"output":{"task_status":"PENDING","task_id":"task-1"},"request_id":"r"}`))
	})
	mux.HandleFunc("/api/v1/tasks/task-1", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer sk-key", r.Header.Get("Authorization"))
		switch polls.Add(1) {
		case 1:
			_, _ = w.Write([]byte(`{"output":{"task_id":"task-1","task_status":"PENDING"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"output":{"task_id":"task-1","task_status":"RUNNING"}}`))
		default:
			_, _ = w.Write([]byte(`{"output":{"task_id":"task-1","task_status":"SUCCEEDED","video_url":"` + srv.URL + `/v.mp4"},"usage":{"duration":5,"SR":720}}`))
		}
	})
	mux.HandleFunc("/v.mp4", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"), "the result URL is the OSS bucket's: the key does not go there")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("WANMP4"))
	})
	srv = httptest.NewServer(mux)
	return srv
}

func alibabaRequest() modeladapter.VideoRequest {
	return modeladapter.VideoRequest{Prompt: "A kitten on a rooftop.", DurationSeconds: 12, Resolution: "720p", AspectRatio: "9:16"}
}

// alibabaBackends is NewBackends with the default base URL of the vendor, whose host holds the workspace ID: every request is
// rewritten to srv and the host it was addressed to is recorded in hosts.
func alibabaBackends(t *testing.T, srv *httptest.Server, keys memKeys, hosts *[]string) modeladapter.Backends {
	t.Helper()
	target, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return modeladapter.NewBackends(modeladapter.DefaultConfig(), keys, doerFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v.mp4" {
			*hosts = append(*hosts, r.URL.Host)
		}
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return srv.Client().Do(r)
	}), time.Millisecond, modeladapter.WithLocalDownloads())
}

func TestAlibabaTextToVideoSendsOnlyThePromptAndTheParametersAsAsyncTask(t *testing.T) {
	var header http.Header
	var submitted map[string]any
	var polls atomic.Int32
	srv := alibabaServer(t, &header, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})

	res, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

	require.NoError(t, err)
	assert.Equal(t, []byte("WANMP4"), fileBytes(t, res.File))
	assert.Equal(t, "video/mp4", res.MimeType)
	assert.EqualValues(t, 3, polls.Load(), "PENDING and RUNNING are waited for")
	assert.Equal(t, "enable", header.Get("X-DashScope-Async"), "HTTP calls support only asynchronous processing")
	assert.Equal(t, "Bearer sk-key", header.Get("Authorization"))
	assert.Equal(t, map[string]any{
		"model": "wan3.0-video",
		"input": map[string]any{"prompt": "A kitten on a rooftop."},
		"parameters": map[string]any{
			"resolution": "720P", "ratio": "9:16", "duration": float64(12), "audio": true,
		},
	}, submitted, "text-to-video has no media array")
}

func TestAlibabaNamesTheResolutionAsTheAPIDoes(t *testing.T) {
	for mamName, apiName := range map[string]string{"480p": "480P", "720p": "720P", "1080p": "1080P"} {
		var header http.Header
		var submitted map[string]any
		var polls atomic.Int32
		srv := alibabaServer(t, &header, &submitted, &polls)
		b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})
		req := alibabaRequest()
		req.Resolution = mamName

		_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", req)

		require.NoError(t, err, mamName)
		assert.Equal(t, apiName, submitted["parameters"].(map[string]any)["resolution"])
		srv.Close()
	}
}

func mediaItem(kind, mime, data string) map[string]any {
	return map[string]any{"type": kind, "url": dataURI(mime, data)}
}

func TestAlibabaFramesAreImageToVideoAndKeepTheRatioOfTheImage(t *testing.T) {
	var header http.Header
	var submitted map[string]any
	var polls atomic.Int32
	srv := alibabaServer(t, &header, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})
	req := alibabaRequest()
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/png"}
	req.LastFrame = &modeladapter.Input{Data: []byte("LAST"), MimeType: "image/jpeg"}
	req.References = []modeladapter.Input{{Data: []byte("REF"), MimeType: "image/png"}}

	_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", req)

	require.NoError(t, err)
	input := submitted["input"].(map[string]any)
	assert.Equal(t, []any{mediaItem("first_frame", "image/png", "FIRST"), mediaItem("last_frame", "image/jpeg", "LAST")}, input["media"],
		"first / last frame and reference images are mutually exclusive: the references are left out")
	assert.Equal(t, "adaptive", submitted["parameters"].(map[string]any)["ratio"])
}

func TestAlibabaFirstFrameAloneIsImageToVideo(t *testing.T) {
	var header http.Header
	var submitted map[string]any
	var polls atomic.Int32
	srv := alibabaServer(t, &header, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})
	req := alibabaRequest()
	req.FirstFrame = &modeladapter.Input{Data: []byte("FIRST"), MimeType: "image/webp"}

	_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", req)

	require.NoError(t, err)
	assert.Equal(t, []any{mediaItem("first_frame", "image/webp", "FIRST")}, submitted["input"].(map[string]any)["media"])
}

func TestAlibabaReferenceImagesAreReferenceToVideoWithTheRatioOfTheShot(t *testing.T) {
	var header http.Header
	var submitted map[string]any
	var polls atomic.Int32
	srv := alibabaServer(t, &header, &submitted, &polls)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})
	req := alibabaRequest()
	req.References = []modeladapter.Input{{Data: []byte("A"), MimeType: "image/png"}, {Data: []byte("B"), MimeType: "image/jpeg"}}

	_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", req)

	require.NoError(t, err)
	assert.Equal(t, []any{mediaItem("reference_image", "image/png", "A"), mediaItem("reference_image", "image/jpeg", "B")}, submitted["input"].(map[string]any)["media"])
	assert.Equal(t, "9:16", submitted["parameters"].(map[string]any)["ratio"])
}

func TestAlibabaDefaultBaseURLIsTheSingaporeWorkspaceHostOfTheKey(t *testing.T) {
	var header http.Header
	var submitted map[string]any
	var polls atomic.Int32
	srv := alibabaServer(t, &header, &submitted, &polls)
	defer srv.Close()
	var hosts []string
	b := alibabaBackends(t, srv, memKeys{"alibaba": "llm-ab12:sk-key"}, &hosts)

	_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

	require.NoError(t, err)
	require.NotEmpty(t, hosts)
	for _, h := range hosts {
		assert.Equal(t, "llm-ab12.ap-southeast-1.maas.aliyuncs.com", h)
	}
	assert.Equal(t, "Bearer sk-key", header.Get("Authorization"), "the workspace ID is not part of the bearer token")
}

func TestAlibabaKeyWithoutAWorkspaceIDOrWithAHostLikeOneDoesNotCallTheAPI(t *testing.T) {
	for name, key := range map[string]string{
		"no workspace":      "sk-key",
		"empty workspace":   ":sk-key",
		"empty key":         "llm-ab12:",
		"path in host":      "evil.example.com/x:sk-key",
		"userinfo in host":  "a@evil.example.com:sk-key",
		"dot in workspace":  "a.b:sk-key",
		"leading hyphen":    "-abc:sk-key",
		"whitespace":        "llm ab:sk-key",
		"newline in host":   "llm\nab:sk-key",
		"unicode in domain": "llm-ａｂ:sk-key",
	} {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
		var hosts []string
		b := alibabaBackends(t, srv, memKeys{"alibaba": key}, &hosts)

		_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

		require.ErrorIs(t, err, modeladapter.ErrNoAPIKey, name)
		assert.Contains(t, err.Error(), "WORKSPACE_ID:API_KEY", name)
		assert.Zero(t, hits.Load(), name)
		assert.Empty(t, hosts, "no request leaves for a host built from %q", key)
		srv.Close()
	}
}

func TestAlibabaWithoutAKeyDoesNotCallTheAPI(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"minimax": "another vendor's key"})

	_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

	require.ErrorIs(t, err, modeladapter.ErrNoAPIKey)
	assert.Zero(t, hits.Load())
}

func TestAlibabaFailedCanceledOrUnknownTaskIsARejectionNotARetry(t *testing.T) {
	for _, status := range []string{"FAILED", "CANCELED", "UNKNOWN"} {
		var creates atomic.Int32
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/services/aigc/video-generation/video-synthesis", func(w http.ResponseWriter, _ *http.Request) {
			creates.Add(1)
			_, _ = w.Write([]byte(`{"output":{"task_status":"PENDING","task_id":"t1"}}`))
		})
		mux.HandleFunc("/api/v1/tasks/t1", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"output":{"task_id":"t1","task_status":"` + status + `","code":"DataInspectionFailed","message":"inappropriate content"}}`))
		})
		srv := httptest.NewServer(mux)
		b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})

		_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

		require.Error(t, err, status)
		assert.True(t, errors.Is(err, modeladapter.ErrRejected), status)
		assert.Contains(t, err.Error(), status)
		assert.EqualValues(t, 1, creates.Load(), "the failed job is not submitted again (billing)")
		srv.Close()
	}
}

func TestAlibabaRefusedCreateCallIsClassifiedByStatusAndErrorBody(t *testing.T) {
	for status, check := range map[int]func(error) bool{
		http.StatusUnauthorized:        func(err error) bool { return errors.Is(err, modeladapter.ErrRejected) },
		http.StatusBadRequest:          func(err error) bool { return errors.Is(err, modeladapter.ErrRejected) },
		http.StatusTooManyRequests:     modeladapter.RateLimited,
		http.StatusInternalServerError: modeladapter.IsUnavailable,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"code":"InvalidApiKey","message":"Invalid API-key provided.","request_id":"r"}`))
		}))
		b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})

		_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

		require.Error(t, err, status)
		assert.True(t, check(err), "HTTP %d: %v", status, err)
		srv.Close()
	}
}

func TestAlibabaAnErrorBodyWithoutATaskIDOn200IsARejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":"InvalidParameter","message":"The two modes are mutually exclusive.","request_id":"r"}`))
	}))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"alibaba": "sk-key"})

	_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

	require.Error(t, err)
	assert.True(t, errors.Is(err, modeladapter.ErrRejected))
	assert.Contains(t, err.Error(), "InvalidParameter")
}

func TestAlibabaResultURLMustBeAnHTTPSURLOfAPublicHost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/services/aigc/video-generation/video-synthesis", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_id":"t1","task_status":"PENDING"}}`))
	})
	mux.HandleFunc("/api/v1/tasks/t1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_id":"t1","task_status":"SUCCEEDED","video_url":"http://169.254.169.254/v.mp4"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var sent []string
	b := strictBackendsFor(t, srv, memKeys{"alibaba": "sk-key"}, &sent)

	_, err := b.Video[domain.ModelWan3].GenerateVideo(context.Background(), "wan3.0-video", alibabaRequest())

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Contains(t, pe.Message, "result URL")
	for _, u := range sent {
		assert.NotContains(t, u, "169.254.169.254", "no request may go to the metadata address")
	}
}
