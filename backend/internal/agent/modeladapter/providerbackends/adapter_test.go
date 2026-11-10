package providerbackends_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func adapter(t *testing.T, srv *httptest.Server, p viaprovider.Provider, keys map[string]string) *modeladapter.Adapter {
	t.Helper()
	cfg, b := backends(t, srv, p, keys)
	return modeladapter.New(cfg, b, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
}

func TestAdapterRunsKling3ThroughEachAggregatorAndEstimatesAtItsPrice(t *testing.T) {
	// One fake that speaks the three APIs by path; the provider chosen decides which part is used.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/videos": // OpenRouter
			_, _ = w.Write([]byte(`{"id":"o1","status":"pending"}`))
		case r.URL.Path == "/videos/o1":
			_, _ = w.Write([]byte(`{"id":"o1","status":"completed"}`))
		case r.URL.Path == "/videos/o1/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		case r.Method == http.MethodPost && r.URL.Path == "/fal-ai/kling-video/v3/standard/text-to-video": // fal.ai
			_, _ = w.Write([]byte(`{"request_id":"f1"}`))
		case r.URL.Path == "/fal-ai/kling-video/requests/f1/status":
			_, _ = w.Write([]byte(`{"status":"COMPLETED"}`))
		case r.URL.Path == "/fal-ai/kling-video/requests/f1":
			_, _ = w.Write([]byte(`{"video":{"url":"http://` + r.Host + `/files/out.mp4"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/models/kwaivgi/kling-v3-video/predictions": // Replicate
			_, _ = w.Write([]byte(`{"id":"r1"}`))
		case r.URL.Path == "/predictions/r1":
			_, _ = w.Write([]byte(`{"id":"r1","status":"succeeded","output":"http://` + r.Host + `/files/out.mp4"}`))
		case r.URL.Path == "/files/out.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	target := modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}
	job := modeladapter.VideoJob{Spec: domain.ShotSpec{ShotObjective: "A fox runs"}, DurationSeconds: 5, MaxTime: time.Minute}

	for p, price := range map[viaprovider.Provider]float64{viaprovider.OpenRouter: 0.084, viaprovider.Fal: 0.084, viaprovider.Replicate: 0.168} {
		a := adapter(t, srv, p, map[string]string{p.Vendor(): "dummy-key"})
		require.Empty(t, a.Unavailable(domain.ModelKling3), "%s: the aggregator's key is the only one needed", p)

		out, err := a.GenerateVideo(context.Background(), target, target, job)

		require.NoError(t, err, string(p))
		readVideo(t, out.VideoResult)
		assert.False(t, out.FellBack)
		assert.InDelta(t, price*5, out.EstimatedCost, 1e-9, "%s: the estimate is at the provider's price", p)
	}
}

func TestAdapterSaysWhyAModelIsUnavailableWithAProvider(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	missing := adapter(t, srv, viaprovider.Fal, map[string]string{"kling": "A:S", "kling4": "k"}) // the maker's keys do not count
	assert.Equal(t, domain.ExcludedNoBackend, missing.Unavailable(domain.ModelKling3))
	assert.Equal(t, domain.ExcludedNoBackend, missing.Unavailable(domain.ModelKling4))
	assert.Equal(t, domain.ExcludedNoBackend, missing.Unavailable(domain.ModelSeedance2), "a model of no aggregator keeps its own rule: its own key is not stored")

	openRouter := adapter(t, srv, viaprovider.OpenRouter, map[string]string{"openrouter": "k", "kling4": "k"})
	assert.Empty(t, openRouter.Unavailable(domain.ModelKling3))
	assert.Equal(t, domain.ExcludedProvider, openRouter.Unavailable(domain.ModelKling4), "OpenRouter has no Kling 4.0, whatever keys there are")

	official := adapter(t, srv, viaprovider.Official, map[string]string{"kling4": "k"})
	assert.Empty(t, official.Unavailable(domain.ModelKling4))
}

func TestAKlingModelTheProviderDoesNotServeFallsOverToTheAlternative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/videos":
			_, _ = w.Write([]byte(`{"id":"o1"}`))
		case r.URL.Path == "/videos/o1":
			_, _ = w.Write([]byte(`{"status":"completed"}`))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		}
	}))
	defer srv.Close()
	a := adapter(t, srv, viaprovider.OpenRouter, map[string]string{"openrouter": "k"})
	k4 := modeladapter.VideoTarget{Model: domain.ModelKling4, Resolution: "720p"}
	k3 := modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}

	out, err := a.GenerateVideo(context.Background(), k4, k3, modeladapter.VideoJob{Spec: domain.ShotSpec{ShotObjective: "x"}, DurationSeconds: 5, MaxTime: time.Minute})

	require.NoError(t, err)
	assert.True(t, out.FellBack)
	assert.Equal(t, domain.ModelKling3, out.Model)
	out.File.Discard()
}
