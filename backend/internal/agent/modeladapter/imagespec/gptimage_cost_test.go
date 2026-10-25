package imagespec_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// GPT Image 2 is priced by the quality that is sent and by the size (spec/architecture/model-adapters.md §9.2).

func loadModels(t *testing.T, body string) modeladapter.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	cfg, err := modeladapter.LoadConfig(path)
	require.NoError(t, err)
	return cfg
}

func TestTheDefaultEstimateOfGPTImage2IsNotZero(t *testing.T) {
	cfg := modeladapter.DefaultConfig()

	assert.InDelta(t, 0.053, cfg.ImageCost(domain.ModelGPTImage2), 1e-9, "the dearest size of the default profile")
	assert.InDelta(t, 0.041, cfg.ImageCostFor(domain.ModelGPTImage2, "16:9"), 1e-9)
	adapter := modeladapter.New(cfg, modeladapter.Backends{})
	assert.InDelta(t, 0.053, adapter.EstimateImage(modeladapter.PurposeNewReference), 1e-9,
		"the budget check holds GPT Image 2's price, not the alternative's $0.0336")
}

func TestTheConfiguredProfileSetsTheQualitySentAndTheEstimate(t *testing.T) {
	for quality, want := range map[string]float64{"low": 0.005, "medium": 0.041, "high": 0.165} {
		t.Run(quality, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}]}`))
			}))
			defer srv.Close()
			cfg := loadModels(t, `{"models":{"gpt-image-2":{"quality":"`+quality+`"}}}`)
			for v, vc := range cfg.Vendors {
				vc.BaseURL = srv.URL
				cfg.Vendors[v] = vc
			}
			backends := modeladapter.NewBackends(cfg, memKeys{"openai": "sk-test"}, srv.Client(), 0, modeladapter.WithLocalDownloads())
			adapter := modeladapter.New(cfg, backends)

			out, err := adapter.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "a bottle", AspectRatio: "16:9"})

			require.NoError(t, err)
			assert.Equal(t, quality, body["quality"])
			assert.InDelta(t, want, out.EstimatedCost, 1e-9)
			assert.Nil(t, out.ActualCost, "the response had no usage")
		})
	}
}

func TestTheActualCostComesFromTheUsageOfTheResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}],
			"usage":{"total_tokens":1368,"input_tokens":68,"output_tokens":1300,"input_tokens_details":{"text_tokens":68,"image_tokens":0}}}`))
	}))
	defer srv.Close()
	cfg := modeladapter.DefaultConfig()
	for v, vc := range cfg.Vendors {
		vc.BaseURL = srv.URL
		cfg.Vendors[v] = vc
	}
	backends := modeladapter.NewBackends(cfg, memKeys{"openai": "sk-test"}, srv.Client(), 0, modeladapter.WithLocalDownloads())
	adapter := modeladapter.New(cfg, backends)

	out, err := adapter.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "a bottle", AspectRatio: "16:9"})

	require.NoError(t, err)
	require.NotNil(t, out.ActualCost, "it is what the cost ledger records as actual_cost")
	assert.InDelta(t, (68*5.0+1300*30.0)/1e6, *out.ActualCost, 1e-12)
	assert.InDelta(t, 0.041, out.EstimatedCost, 1e-9)
}

func TestAConfigFileWrittenBeforeTheProfileKeepsTheDefaultProfile(t *testing.T) {
	cfg := loadModels(t, `{"models":{"gpt-image-2":{"model":"gpt-image-2"}}}`)

	assert.Equal(t, "medium", cfg.Models[domain.ModelGPTImage2].Quality)
	assert.InDelta(t, 0.041, cfg.ImageCostFor(domain.ModelGPTImage2, "16:9"), 1e-9)
}

func TestAPriceTheUserSetWinsOverTheTable(t *testing.T) {
	cfg := loadModels(t, `{"models":{"gpt-image-2":{"cost_per_image_usd":0.2}}}`)

	assert.InDelta(t, 0.2, cfg.ImageCostFor(domain.ModelGPTImage2, "16:9"), 1e-9)
	assert.InDelta(t, 0.2, cfg.ImageCost(domain.ModelGPTImage2), 1e-9)
}

func TestAnotherAPIModelIDHasNoTablePrice(t *testing.T) {
	cfg := loadModels(t, `{"models":{"gpt-image-2":{"model":"gpt-image-1.5"}}}`)

	assert.Zero(t, cfg.ImageCost(domain.ModelGPTImage2), "the table is gpt-image-2's: no price is invented for another model")
}
