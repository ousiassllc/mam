package providerbackends_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestFalKling3SubmitsToTheQueueAndReadsTheResult(t *testing.T) {
	var mu sync.Mutex
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := seen{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization")}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &s.body)
		}
		mu.Lock()
		got = append(got, s)
		n := len(got)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"request_id":"req-1","status":"IN_QUEUE"}`))
		case r.URL.Path == "/fal-ai/kling-video/requests/req-1/status":
			status := "IN_PROGRESS"
			if n >= 3 {
				status = "COMPLETED"
			}
			_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
		case r.URL.Path == "/fal-ai/kling-video/requests/req-1":
			_, _ = w.Write([]byte(`{"video":{"url":"http://` + r.Host + `/files/out.mp4","content_type":"video/mp4"}}`))
		case r.URL.Path == "/files/out.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Fal, map[string]string{"fal": "fal-dummy-key"})

	res, err := generate(t, b, domain.ModelKling3, modeladapter.VideoRequest{
		Prompt: "p", NegativePrompt: "blur", DurationSeconds: 5, Resolution: "1080p", AspectRatio: "16:9", FirstFrame: frame("A"), LastFrame: frame("B"),
	})

	require.NoError(t, err)
	readVideo(t, res)
	mu.Lock()
	defer mu.Unlock()
	submit := got[0]
	assert.Equal(t, "/fal-ai/kling-video/v3/pro/image-to-video", submit.path, "1080p is the pro tier, a first frame makes it image-to-video")
	assert.Equal(t, "Key fal-dummy-key", submit.auth)
	assert.Equal(t, "5", submit.body["duration"], "Kling 3.0 on fal takes the length as a string")
	assert.Equal(t, false, submit.body["generate_audio"])
	assert.Equal(t, "blur", submit.body["negative_prompt"])
	assert.Equal(t, dataURI("A"), submit.body["start_image_url"])
	assert.Equal(t, dataURI("B"), submit.body["end_image_url"])
	assert.NotContains(t, submit.body, "aspect_ratio")
	for _, r := range got[1:] {
		if r.path != "/files/out.mp4" {
			assert.Equal(t, "Key fal-dummy-key", r.auth)
		} else {
			assert.Empty(t, r.auth, "the key does not go to the file host")
		}
	}
}

func TestFalKling4UsesTheNewEndpointWithAnIntegerLengthAndTheResolution(t *testing.T) {
	var submit seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			submit = seen{method: r.Method, path: r.URL.Path}
			_ = json.NewDecoder(r.Body).Decode(&submit.body)
			_, _ = w.Write([]byte(`{"request_id":"r4"}`))
		case r.URL.Path == "/kling/v4/requests/r4/status":
			_, _ = w.Write([]byte(`{"status":"COMPLETED"}`))
		case r.URL.Path == "/kling/v4/requests/r4":
			_, _ = w.Write([]byte(`{"video":{"url":"http://` + r.Host + `/files/out.mp4"}}`))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		}
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Fal, map[string]string{"fal": "k"})

	res, err := generate(t, b, domain.ModelKling4, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 12, Resolution: "720p", AspectRatio: "9:16"})

	require.NoError(t, err)
	readVideo(t, res)
	assert.Equal(t, "/kling/v4/text-to-video", submit.path)
	assert.EqualValues(t, 12, submit.body["duration"])
	assert.Equal(t, "720p", submit.body["resolution"])
	assert.Equal(t, "9:16", submit.body["aspect_ratio"])
	assert.NotContains(t, submit.body, "generate_audio")
}

func TestFalAnErrorOfTheModelIsAnErrorOfTheCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"request_id":"r"}`))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"status":"COMPLETED"}`))
		default:
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"detail":"content policy"}`))
		}
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Fal, map[string]string{"fal": "k"})

	_, err := generate(t, b, domain.ModelKling3, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

	require.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Contains(t, err.Error(), "content policy")
}
