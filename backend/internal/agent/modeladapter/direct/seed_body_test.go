package direct_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

type keys map[string]string

func (m keys) Set(p, k string) error { m[p] = k; return nil }
func (m keys) Get(p string) (string, error) {
	if k, ok := m[p]; ok {
		return k, nil
	}
	return "", credential.ErrNotFound
}
func (m keys) Delete(p string) error      { delete(m, p); return nil }
func (m keys) Has(p string) (bool, error) { _, ok := m[p]; return ok, nil }

// bodies records the JSON body of every POST by path.
type bodies struct {
	mu sync.Mutex
	m  map[string]map[string]any
}

func (b *bodies) get(path string) map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m[path]
}

// TestSeedIsPutIntoTheBodyOfFluxAndRunway runs the real vendor backends against a fake server: the seed the generation test
// chose is in the request of FLUX.2 and Runway Gen-4.5, and a request without one has no seed field.
func TestSeedIsPutIntoTheBodyOfFluxAndRunway(t *testing.T) {
	got := &bodies{m: map[string]map[string]any{}}
	mux := http.NewServeMux()
	var srv *httptest.Server
	record := func(w http.ResponseWriter, r *http.Request, reply string) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got.mu.Lock()
		got.m[r.URL.Path] = body
		got.mu.Unlock()
		_, _ = w.Write([]byte(reply))
	}
	mux.HandleFunc("/v1/flux-2-pro", func(w http.ResponseWriter, r *http.Request) {
		record(w, r, `{"id":"j1","polling_url":"`+srv.URL+`/poll"}`)
	})
	mux.HandleFunc("/poll", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + srv.URL + `/file.jpg"}}`))
	})
	mux.HandleFunc("/file.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEG"))
	})
	mux.HandleFunc("/v1/text_to_video", func(w http.ResponseWriter, r *http.Request) { record(w, r, `{"id":"r1"}`) })
	mux.HandleFunc("/v1/tasks/r1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"SUCCEEDED","output":["` + srv.URL + `/r.mp4"]}`))
	})
	mux.HandleFunc("/r.mp4", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("MP4"))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	backends := modeladapter.NewBackends(cfg, keys{"flux": "k", "runway": "k"}, srv.Client(), time.Millisecond, modeladapter.WithLocalDownloads())

	seed := int64(4000000000)
	img := backends.Image[domain.ModelFlux2]
	_, err := img.GenerateImage(t.Context(), "flux-2-pro", modeladapter.ImageRequest{Prompt: "p", AspectRatio: "1:1", Seed: &seed})
	require.NoError(t, err)
	assert.EqualValues(t, 4000000000, got.get("/v1/flux-2-pro")["seed"])
	_, err = img.GenerateImage(t.Context(), "flux-2-pro", modeladapter.ImageRequest{Prompt: "p", AspectRatio: "1:1"})
	require.NoError(t, err)
	assert.NotContains(t, got.get("/v1/flux-2-pro"), "seed")

	vid := backends.Video[domain.ModelRunwayGen45]
	res, err := vid.GenerateVideo(t.Context(), "gen4.5", modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9", Seed: &seed})
	require.NoError(t, err)
	res.File.Discard()
	assert.EqualValues(t, 4000000000, got.get("/v1/text_to_video")["seed"])
}
