package localbackends_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func imageServer(t *testing.T, status int, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func configWith(endpoints map[string]string) modeladapter.Config {
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints, cfg.LocalModels = map[string]localcfg.Endpoint{}, map[string]localcfg.Model{}
	for name, url := range endpoints {
		cfg.Endpoints[name] = localcfg.Endpoint{Kind: localcfg.KindOpenAICompat, BaseURL: url}
		cfg.LocalModels[name+"/m"] = localcfg.Model{Media: localcfg.MediaImage, Model: "srv-model", CostPerImage: 0.01}
	}
	return cfg
}

func newAdapter(t *testing.T, cfg modeladapter.Config) *modeladapter.Adapter {
	t.Helper()
	require.NoError(t, configcheck.Validate(cfg))
	b := localbackends.Build(cfg, credentialtest.New(), localbackends.Options{PollEvery: time.Millisecond, StagingDir: t.TempDir()})
	return modeladapter.New(cfg, b, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
}

// A local model takes part in the image routes like a built-in one: the primary's failure is retried,
// then the Adapter fails over to the alternative, and the result names the model by its identifier.
func TestLocalModelsFailOverLikeBuiltInOnes(t *testing.T) {
	var badHits, goodHits atomic.Int32
	bad, good := imageServer(t, 500, &badHits), imageServer(t, 200, &goodHits)
	cfg := configWith(map[string]string{"bad": bad.URL, "good": good.URL})
	cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: "openai-compat:bad/m"},
		Fallback: modeladapter.ImageTarget{Model: "openai-compat:good/m"},
	}
	a := newAdapter(t, cfg)

	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)

	assert.Equal(t, domain.ModelName("openai-compat:good/m"), out.Model, "take.model_name / cost_ledger_entry.provider carry endpoint and model")
	assert.True(t, out.FellBack)
	assert.Equal(t, png, out.Data)
	assert.Equal(t, 0.01, out.EstimatedCost)
	require.NotNil(t, out.ActualCost)
	assert.Equal(t, int32(cfg.Retry.MaxAttempts), badHits.Load(), "retried like any vendor")
	assert.Equal(t, int32(1), goodHits.Load())
	assert.Equal(t, 1, a.ConcurrencyLimit("endpoint:bad"), "one GPU runs one job at a time by default")
}

func TestReloadAppliesANewEndpointAtOnceAndKeepsTheBreaker(t *testing.T) {
	var hits atomic.Int32
	srv := imageServer(t, 200, &hits)
	a := newAdapter(t, modeladapter.DefaultConfig())
	_, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "p"})
	require.Error(t, err, "no key for the built-in vendors: nothing can run before the endpoint is added")

	cfg := configWith(map[string]string{"box": srv.URL})
	cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
		Primary: modeladapter.ImageTarget{Model: "openai-compat:box/m"}, Fallback: modeladapter.ImageTarget{Model: "openai-compat:box/m"},
	}
	require.NoError(t, configcheck.Validate(cfg))
	a.Reload(cfg, localbackends.Build(cfg, credentialtest.New(), localbackends.Options{PollEvery: time.Millisecond, StagingDir: t.TempDir()}))

	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelName("openai-compat:box/m"), out.Model)
	assert.Equal(t, cfg.Endpoints, a.Config().Endpoints)
}

// Reload keeps the call in flight on the setting it started with.
func TestReloadDoesNotDisturbACallInFlight(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	defer srv.Close()
	cfg := configWith(map[string]string{"box": srv.URL})
	cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
		Primary: modeladapter.ImageTarget{Model: "openai-compat:box/m"}, Fallback: modeladapter.ImageTarget{Model: "openai-compat:box/m"},
	}
	a := newAdapter(t, cfg)
	done := make(chan error, 1)
	go func() {
		_, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "p"})
		done <- err
	}()
	<-started
	a.Reload(modeladapter.DefaultConfig(), modeladapter.Backends{})
	close(release)
	require.NoError(t, <-done)
}

// The registered origin is allowed for the endpoint's own models only: an answer of a built-in vendor that
// names a file on the registered origin (or on any private / tailnet / loopback address) is still refused.
func TestBuiltInVendorsKeepTheStrictDownloadRulesWhenAnEndpointIsRegistered(t *testing.T) {
	var fileHits atomic.Int32
	registered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fileHits.Add(1)
		_, _ = w.Write([]byte("secret"))
	}))
	defer registered.Close()
	for name, resultURL := range map[string]string{
		"registered origin": registered.URL + "/file",
		"private":           "https://192.168.0.10/v.mp4",
		"tailnet":           "https://100.64.0.1/v.mp4",
		"loopback":          "https://127.0.0.1/v.mp4",
		"localhost":         "https://localhost/v.mp4",
		"metadata":          "https://169.254.169.254/latest",
	} {
		t.Run(name, func(t *testing.T) {
			vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/tasks") {
					_, _ = w.Write([]byte(`{"id":"t"}`))
					return
				}
				_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"` + resultURL + `"}}`))
			}))
			defer vendor.Close()
			cfg := configWith(map[string]string{"box": registered.URL})
			seedance := cfg.Vendors[modeladapter.VendorSeedance]
			seedance.BaseURL = vendor.URL
			cfg.Vendors[modeladapter.VendorSeedance] = seedance
			keys := credentialtest.New()
			require.NoError(t, keys.Set("seedance", "k"))
			b := localbackends.Build(cfg, keys, localbackends.Options{PollEvery: time.Millisecond, StagingDir: t.TempDir()})

			_, err := b.Video[domain.ModelSeedance2].GenerateVideo(context.Background(), "m", modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

			var pe *modeladapter.ProviderError
			require.ErrorAs(t, err, &pe)
			assert.Contains(t, pe.Message, "result URL")
		})
	}
	assert.Zero(t, fileHits.Load(), "no built-in vendor reached the registered server")
}

func TestPublishMakesLocalModelsSelectable(t *testing.T) {
	cfg := configWith(map[string]string{"box": "http://127.0.0.1:1"})
	cfg.Endpoints["gpu"] = localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:1"}
	cfg.LocalModels["gpu/v"] = localcfg.Model{Media: localcfg.MediaVideo}
	localbackends.Publish(cfg)
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	assert.Contains(t, domain.ImageModels(), domain.ModelName("openai-compat:box/m"))
	assert.NotContains(t, domain.VideoModels(), domain.ModelName("openai-compat:box/m"))
	assert.Contains(t, domain.VideoModels(), domain.ModelName("comfyui:gpu/v"))
	assert.Equal(t, []domain.ModelName{domain.ModelKling3, domain.ModelSeedance2, domain.ModelRunwayGen45}, domain.VideoModels()[:3], "built-in models come first")
	localbackends.Publish(modeladapter.DefaultConfig())
	assert.NotContains(t, domain.ImageModels(), domain.ModelName("openai-compat:box/m"))
}
