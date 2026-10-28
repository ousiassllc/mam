package backendstest_test

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

const seedance25APIModel = "dreamina-seedance-2-5-260628"

// seedanceServer answers the content generation API: the create call (recording its body) and a task that is running for the
// first poll and then succeeded with the video at /v.mp4.
func seedance25Server(t *testing.T, submitted *map[string]any) *httptest.Server {
	t.Helper()
	var polls atomic.Int32
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/api/v3/contents/generations/tasks", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer k", r.Header.Get("Authorization"))
		_ = json.NewDecoder(r.Body).Decode(submitted)
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
		_, _ = w.Write([]byte("S25MP4"))
	})
	srv = httptest.NewServer(mux)
	return srv
}

func seedance25Request() modeladapter.VideoRequest {
	return modeladapter.VideoRequest{Prompt: "A long walk through six rooms.", DurationSeconds: 30, Resolution: "1080p", AspectRatio: "9:16"}
}

func TestSeedance25SendsTheOutputSpecificationAsFieldsNotAsPromptFlags(t *testing.T) {
	var submitted map[string]any
	srv := seedance25Server(t, &submitted)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	res, err := b.Video[domain.ModelSeedance25].GenerateVideo(context.Background(), seedance25APIModel, seedance25Request())

	require.NoError(t, err)
	assert.Equal(t, []byte("S25MP4"), fileBytes(t, res.File))
	assert.Equal(t, seedance25APIModel, submitted["model"])
	assert.Equal(t, "1080p", submitted["resolution"])
	assert.Equal(t, "9:16", submitted["ratio"])
	assert.EqualValues(t, 30, submitted["duration"])
	assert.Equal(t, true, submitted["generate_audio"])
	assert.NotContains(t, submitted, "omni_reference_task_type")
	content := submitted["content"].([]any)
	require.Len(t, content, 1)
	assert.Equal(t, map[string]any{"type": "text", "text": "A long walk through six rooms."}, content[0], "no --resolution / --duration / --ratio flags")
}

func TestSeedance25FirstFrameRequiresTheAdaptiveRatio(t *testing.T) {
	var submitted map[string]any
	srv := seedance25Server(t, &submitted)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})
	req := seedance25Request()
	req.FirstFrame = &modeladapter.Input{Data: []byte("F"), MimeType: "image/png"}
	req.LastFrame = &modeladapter.Input{Data: []byte("L"), MimeType: "image/png"}
	req.References = []modeladapter.Input{{Data: []byte("R"), MimeType: "image/png"}}

	_, err := b.Video[domain.ModelSeedance25].GenerateVideo(context.Background(), seedance25APIModel, req)

	require.NoError(t, err)
	assert.Equal(t, "adaptive", submitted["ratio"], "the output keeps the ratio of the first frame: any other ratio is an error of the API")
	assert.NotContains(t, submitted, "omni_reference_task_type")
	var roles []string
	for _, item := range submitted["content"].([]any)[1:] {
		roles = append(roles, item.(map[string]any)["role"].(string))
	}
	assert.Equal(t, []string{"first_frame", "last_frame"}, roles, "frames and reference images are never sent together")
}

func TestSeedance25ReferencesAreOmniReferenceImages(t *testing.T) {
	var submitted map[string]any
	srv := seedance25Server(t, &submitted)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})
	req := seedance25Request()
	req.References = []modeladapter.Input{{Data: []byte("A"), MimeType: "image/png"}, {Data: []byte("B"), MimeType: "image/jpeg"}}

	_, err := b.Video[domain.ModelSeedance25].GenerateVideo(context.Background(), seedance25APIModel, req)

	require.NoError(t, err)
	assert.Equal(t, "reference", submitted["omni_reference_task_type"])
	assert.Equal(t, "9:16", submitted["ratio"], "a reference-to-video task has no ratio constraint")
	content := submitted["content"].([]any)
	require.Len(t, content, 3)
	for _, item := range content[1:] {
		assert.Equal(t, "reference_image", item.(map[string]any)["role"])
		assert.True(t, strings.HasPrefix(item.(map[string]any)["image_url"].(map[string]any)["url"].(string), "data:image/"))
	}
}

func TestSeedance2KeepsTheFlagsInThePromptAndSendsNoFields(t *testing.T) {
	var submitted map[string]any
	srv := seedance25Server(t, &submitted)
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{"seedance": "k"})
	req := seedance25Request()
	req.DurationSeconds = 8
	req.Resolution = "720p"

	_, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "seedance-2-0", req)

	require.NoError(t, err)
	assert.Equal(t, "seedance-2-0", submitted["model"], "Seedance 2.0 keeps the model identifier it has")
	for _, field := range []string{"resolution", "ratio", "duration", "generate_audio", "omni_reference_task_type"} {
		assert.NotContains(t, submitted, field)
	}
	text := submitted["content"].([]any)[0].(map[string]any)["text"]
	assert.Equal(t, "A long walk through six rooms. --resolution 720p --duration 8 --ratio 9:16", text)
}

func TestSeedance25AndSeedance2ShareOneKeyAndOneVendor(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	b := backendsFor(t, srv, memKeys{})

	for _, m := range []domain.ModelName{domain.ModelSeedance2, domain.ModelSeedance25} {
		_, err := b.Video[m].GenerateVideo(context.Background(), "x", seedance25Request())
		require.ErrorIs(t, err, modeladapter.ErrNoAPIKey, m)
	}
	assert.Zero(t, hits.Load())
	assert.Equal(t, modeladapter.VendorOf(domain.ModelSeedance2), modeladapter.VendorOf(domain.ModelSeedance25))
}
