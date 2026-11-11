package providerbackends_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestReplicateKling4SendsTheInputAndReadsTheOutputURL(t *testing.T) {
	var submit latest[seen]
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			s := seen{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization")}
			_ = json.NewDecoder(r.Body).Decode(&s.body)
			submit.set(s)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"pred1","status":"starting"}`))
		case r.URL.Path == "/predictions/pred1":
			if polls.Add(1) < 2 {
				_, _ = w.Write([]byte(`{"id":"pred1","status":"processing"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"pred1","status":"succeeded","output":"http://` + r.Host + `/files/out.mp4"}`))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		}
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Replicate, map[string]string{"replicate": "r8_dummy"})

	res, err := generate(t, b, domain.ModelKling4, modeladapter.VideoRequest{
		Prompt: "p", DurationSeconds: 6, Resolution: "1080p", AspectRatio: "16:9", FirstFrame: frame("A"),
	})

	require.NoError(t, err)
	readVideo(t, res)
	got := submit.get()
	assert.Equal(t, "/models/kwaivgi/kling-v4.0/predictions", got.path)
	assert.Equal(t, "Bearer r8_dummy", got.auth)
	input, _ := got.body["input"].(map[string]any)
	assert.Equal(t, "1080p", input["resolution"])
	assert.EqualValues(t, 6, input["duration"])
	assert.Equal(t, dataURI("A"), input["start_image"])
	assert.NotContains(t, input, "end_image")
	assert.NotContains(t, input, "aspect_ratio", "the first frame sets the ratio")
}

func TestReplicateKling3UsesTheModeAndAcceptsAListOutput(t *testing.T) {
	var submitted latest[map[string]any]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			assert.Equal(t, "/models/kwaivgi/kling-v3-video/predictions", r.URL.Path)
			var body struct {
				Input map[string]any `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			submitted.set(body.Input)
			_, _ = w.Write([]byte(`{"id":"p"}`))
		case r.URL.Path == "/predictions/p":
			_, _ = w.Write([]byte(`{"id":"p","status":"succeeded","output":["http://` + r.Host + `/files/out.mp4"]}`))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		}
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Replicate, map[string]string{"replicate": "k"})

	res, err := generate(t, b, domain.ModelKling3, modeladapter.VideoRequest{
		Prompt: "p", NegativePrompt: "blur", DurationSeconds: 9, Resolution: "720p", AspectRatio: "1:1",
	})

	require.NoError(t, err)
	readVideo(t, res)
	input := submitted.get()
	assert.Equal(t, "standard", input["mode"], "720p is the standard tier")
	assert.Equal(t, false, input["generate_audio"])
	assert.Equal(t, "blur", input["negative_prompt"])
	assert.Equal(t, "1:1", input["aspect_ratio"])
}

func TestReplicateFailedPredictionIsRefusedWithItsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"p"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"p","status":"failed","error":"NSFW content detected"}`))
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.Replicate, map[string]string{"replicate": "k"})

	_, err := generate(t, b, domain.ModelKling4, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

	require.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.Contains(t, err.Error(), "NSFW content detected")
}
