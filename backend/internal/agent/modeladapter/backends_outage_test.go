package modeladapter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

var seedanceReq = modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 4, Resolution: "720p", AspectRatio: "16:9"}

// A vendor outage longer than the old limit of three tries must not lose the output of a job that is billed already.
func TestSubmittedJobSurvivesAnOutageOfStatusAndDownloadWithoutBeingSubmittedAgain(t *testing.T) {
	srv, submits, polls, downloads := seedanceServer(t, []int{503, 503, 503, 503, 503, 503, 0}, []int{502, 502, 502, 502, 502, 502, 0})
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	res, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", seedanceReq)

	require.NoError(t, err)
	assert.Equal(t, []byte("MP4"), fileBytes(t, res.File))
	assert.EqualValues(t, 1, submits.Load())
	assert.EqualValues(t, 7, polls.Load())
	assert.EqualValues(t, 7, downloads.Load())
}

func TestPollRetriesLastUntilTheDeadlineOfTheCall(t *testing.T) {
	srv, _, polls, _ := seedanceServer(t, []int{http.StatusServiceUnavailable}, []int{0})
	b := backendsFor(t, srv, memKeys{"seedance": "k"})

	_, err := b.Video[domain.ModelSeedance2].GenerateVideo(testenv.EndsAfterFirstRequest(t, polls, 100*time.Millisecond), "m", seedanceReq)

	var pe *modeladapter.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, http.StatusServiceUnavailable, pe.Status)
	assert.Greater(t, polls.Load(), int32(3), "not given up after a fixed number of tries")
}

func TestAdapterDoesNotSubmitAgainWhenPollingKeepsFailing(t *testing.T) {
	srv, submits, polls, _ := seedanceServer(t, []int{http.StatusServiceUnavailable}, []int{0})
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	a := newAdapter(cfg, modeladapter.NewBackends(cfg, memKeys{"seedance": "k"}, srv.Client(), time.Millisecond, modeladapter.WithLocalDownloads()))

	_, err := a.GenerateVideo(testenv.EndsAfterFirstRequest(t, polls, 100*time.Millisecond), seedanceTarget, seedanceTarget, baseJob())

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

func TestDefaultClientDoesNotForwardTheKeyOnARedirectToAnotherHost(t *testing.T) {
	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits.Add(1) }))
	defer foreign.Close()
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL+"/steal", http.StatusFound)
	}))
	defer vendor.Close()
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = vendor.URL
		cfg.Vendors[v] = vc
	}
	b := modeladapter.NewBackends(cfg, memKeys{"flux": "secret", "seedance": "secret"}, nil, time.Millisecond)

	_, err := b.Image[domain.ModelFlux2].GenerateImage(context.Background(), "flux-2-pro", modeladapter.ImageRequest{Prompt: "p"})
	require.Error(t, err)
	_, err = b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", seedanceReq)
	require.Error(t, err)

	assert.Zero(t, foreignHits.Load())
}

// fileBytes reads the content of a generated video and discards the staged file.
func fileBytes(t *testing.T, b modeladapter.Blob) []byte {
	t.Helper()
	data, err := b.Bytes()
	require.NoError(t, err)
	b.Discard()
	return data
}

func TestUnavailableOfARealBackendFollowsTheStoredAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	defer srv.Close()
	keys := memKeys{"seedance": "k"}
	a := modeladapter.New(modeladapter.DefaultConfig(), backendsFor(t, srv, keys))

	assert.Empty(t, a.Unavailable(domain.ModelSeedance2))
	assert.Equal(t, domain.ExcludedNoBackend, a.Unavailable(domain.ModelKling3), "no Kling key is stored")
	assert.Equal(t, domain.ExcludedNoBackend, a.Unavailable(domain.ModelRunwayGen45))

	keys["kling"] = "k" // keys are added while the server runs
	assert.Empty(t, a.Unavailable(domain.ModelKling3))
}
