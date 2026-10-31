package chatgptimage_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/configcheck"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localbackends"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
)

// fallbackServer is an OpenAI-compatible endpoint that answers every image request with png.
func fallbackServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// subscriptionAdapter routes new references to the subscription's image model, with an endpoint model as the alternative.
func subscriptionAdapter(t *testing.T, codexURL, fallbackURL string, src chatgptimage.CredentialSource) *modeladapter.Adapter {
	t.Helper()
	cfg := modeladapter.DefaultConfig()
	cfg.Endpoints = map[string]localcfg.Endpoint{"fb": {Kind: localcfg.KindOpenAICompat, BaseURL: fallbackURL}}
	cfg.LocalModels = map[string]localcfg.Model{"fb/m": {Media: localcfg.MediaImage, Model: "srv", CostPerImage: 0.01}}
	cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
		Primary:  modeladapter.ImageTarget{Model: chatgptid.Model},
		Fallback: modeladapter.ImageTarget{Model: "openai-compat:fb/m"},
	}
	require.NoError(t, configcheck.Validate(cfg), "the identifier is a valid route target")
	b := localbackends.Build(cfg, credentialtest.New(), localbackends.Options{
		StagingDir:   t.TempDir(),
		ChatGPTImage: chatgptimage.New(src, chatgptimage.Options{BaseURL: codexURL, Stall: time.Second}),
	})
	return modeladapter.New(cfg, b, modeladapter.WithSleep(func(context.Context, time.Duration) error { return nil }))
}

func TestSubscriptionImageModelIsRoutedThroughTheAdapter(t *testing.T) {
	c := newCodex(t, func(w http.ResponseWriter, _ http.Flusher) {
		data(w, imageDone("png"), `{"type":"response.completed"}`)
	})
	var fbHits atomic.Int32
	a := subscriptionAdapter(t, c.URL, fallbackServer(t, &fbHits).URL, &source{})

	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{
		Prompt: "a fox", Inputs: []modeladapter.Input{{Data: []byte{1}, MimeType: "image/png"}},
	})
	require.NoError(t, err)
	assert.Equal(t, chatgptid.Model, out.Model, "the ledger names the model by its identifier")
	assert.False(t, out.FellBack)
	assert.Zero(t, out.EstimatedCost, "the plan pays: the ledger row is written with 0 USD")
	assert.InDelta(t, 0.01, a.EstimateImage(modeladapter.PurposeNewReference), 1e-9, "the budget holds the dearer of the two models")
	assert.Equal(t, int32(1), c.n.Load())
	assert.Zero(t, fbHits.Load())
	assert.Len(t, c.body["input"], 1)
	assert.True(t, a.Config().AcceptsImages(chatgptid.Model), "reference images are sent as input_image")
	assert.Equal(t, modeladapter.Vendor(chatgptid.Vendor), modeladapter.VendorOf(chatgptid.Model), "own breaker and limiter")
}

func TestSubscriptionImageModelOffMeansNoCallAndTheAlternativeServes(t *testing.T) {
	c := newCodex(t, func(w http.ResponseWriter, _ http.Flusher) { data(w, imageDone("png")) })
	var fbHits atomic.Int32
	a := subscriptionAdapter(t, c.URL, fallbackServer(t, &fbHits).URL, &source{err: chatgpt.ErrImageDisabled})

	out, err := a.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "a fox"})
	require.NoError(t, err)
	assert.Equal(t, domain.ModelName("openai-compat:fb/m"), out.Model)
	assert.True(t, out.FellBack)
	assert.Zero(t, c.n.Load(), "OFF: nothing is sent to chatgpt.com")
	assert.Equal(t, int32(1), fbHits.Load())
}

func TestSubscriptionImageModelIsListedOnlyWhileItIsOn(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	on := false
	o := localbackends.Options{ChatGPTImage: chatgptimage.New(&source{}, chatgptimage.Options{}), ChatGPTImageEnabled: func() bool { return on }}
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })

	o.Publish(cfg)
	assert.NotContains(t, domain.ImageModels(), chatgptid.Model)
	on = true
	o.Publish(cfg)
	assert.Contains(t, domain.ImageModels(), chatgptid.Model)
	on = false
	o.Publish(cfg)
	assert.NotContains(t, domain.ImageModels(), chatgptid.Model)
}

// The generation test's model list and the estimate's breakdown show the model by its display name (Info.Label), not by
// the identifier "gpt-image-2:chatgpt-subscription".
func TestSubscriptionImageModelHasADisplayName(t *testing.T) {
	on := true
	o := localbackends.Options{ChatGPTImage: chatgptimage.New(&source{}, chatgptimage.Options{}), ChatGPTImageEnabled: func() bool { return on }}
	t.Cleanup(func() { localbackends.Publish(modeladapter.DefaultConfig()) })
	cfg := modeladapter.DefaultConfig()
	a := modeladapter.New(cfg, localbackends.Build(cfg, credentialtest.New(), o))
	o.Publish(cfg)

	info, ok := modelcatalog.Catalog{Adapter: a}.Model("image", string(chatgptid.Model))
	require.True(t, ok, "the model is listed while it is on")
	assert.Equal(t, chatgptid.Label, info.Label)
	assert.NotEqual(t, string(chatgptid.Model), info.Label)
	assert.Contains(t, info.Label, "ChatGPT", "the label says how the model is reached")

	on = false
	o.Publish(cfg)
	assert.Equal(t, chatgptid.Label, modelreg.Label(string(chatgptid.Model)), "a batch made while it was on still names it after it is turned off")
}
