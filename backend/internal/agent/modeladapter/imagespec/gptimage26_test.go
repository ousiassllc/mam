package imagespec_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// GPT Image 2.6 Flare and Sunburst (#1096). Their API model IDs, their per-token prices and their endpoints (images/generations and
// images/edits) are those of OpenAI's model pages and pricing page (spec/architecture/model-adapters.md §9.2, read 2026-11-06).
// OpenAI publishes no table of the price of one image for them, so the estimate is the price the user sets, never a guess.

var gpt26 = []struct {
	model domain.ModelName
	apiID string
}{
	{domain.ModelGPTImage26Flare, "gpt-image-2.6-flare"},
	{domain.ModelGPTImage26Sunburst, "gpt-image-2.6-sunburst"},
}

func TestTheDefaultImageModelIsFlareAndGPTImage2StaysSelectable(t *testing.T) {
	cfg := modeladapter.DefaultConfig()

	assert.Equal(t, domain.ModelGPTImage26Flare, cfg.Images[modeladapter.PurposeNewReference].Primary.Model)
	assert.Equal(t, domain.ModelGPTImage26Flare, cfg.Images[modeladapter.PurposeUnifiedOpenAI].Primary.Model)
	assert.Equal(t, domain.ModelGPTImage26Flare, cfg.Images[modeladapter.PurposeEditReference].Fallback.Model)
	for _, m := range []domain.ModelName{domain.ModelGPTImage2, domain.ModelGPTImage26Flare, domain.ModelGPTImage26Sunburst} {
		assert.Contains(t, domain.ImageModels(), m, "it can be chosen")
		assert.NotEmpty(t, cfg.Models[m].Model, "and has its own settings")
		assert.Equal(t, modeladapter.VendorOpenAI, modeladapter.VendorOf(m))
		assert.True(t, modeladapter.AcceptsImageInputs(m), "it takes references (images/edits)")
	}
	for _, e := range gpt26 {
		assert.Equal(t, e.apiID, cfg.Models[e.model].Model)
	}
}

func TestAnExplicitlySavedRouteIsNotReplacedByTheNewDefault(t *testing.T) {
	cfg := loadModels(t, `{"images":{"new_reference":{"primary":{"model":"gpt-image-2","fast":false},"fallback":{"model":"nano-banana","fast":false}}}}`)
	assert.Equal(t, domain.ModelGPTImage2, cfg.Images[modeladapter.PurposeNewReference].Primary.Model)

	untouched := modeladapter.DefaultConfig()
	assert.Equal(t, domain.ModelGPTImage26Flare, untouched.Images[modeladapter.PurposeNewReference].Primary.Model, "an unset route follows the default")
}

func TestTheSelectedGPTImage26ModelIDIsSentAndRecorded(t *testing.T) {
	for _, e := range gpt26 {
		t.Run(string(e.model), func(t *testing.T) {
			var gotPath string
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}]}`))
			}))
			defer srv.Close()
			cfg := modeladapter.DefaultConfig()
			cfg.Images[modeladapter.PurposeNewReference] = modeladapter.ImageRoute{
				Primary: modeladapter.ImageTarget{Model: e.model}, Fallback: modeladapter.ImageTarget{Model: domain.ModelNanoBanana},
			}
			for v, vc := range cfg.Vendors {
				vc.BaseURL = srv.URL
				cfg.Vendors[v] = vc
			}
			adapter := modeladapter.New(cfg, modeladapter.NewBackends(cfg, memKeys{"openai": "sk-test"}, srv.Client(), 0, modeladapter.WithLocalDownloads()))

			out, err := adapter.GenerateImage(context.Background(), modeladapter.PurposeNewReference, modeladapter.ImageRequest{Prompt: "a bottle", AspectRatio: "16:9"})

			require.NoError(t, err)
			assert.Equal(t, e.model, out.Model, "the model that drew it, for the result and the ledger")
			assert.False(t, out.FellBack)
			assert.Equal(t, "/v1/images/generations", gotPath)
			assert.Equal(t, map[string]any{"model": e.apiID, "prompt": "a bottle", "size": "1536x1024", "quality": "medium", "n": float64(1)}, body,
				"the model ID of the selection and nothing mam does not know the model to accept (no complexity, output_format, markup)")
			assert.Zero(t, out.EstimatedCost, "no table of the price of one image is published: the estimate is unknown, not invented")
		})
	}
}

func TestGPTImage26EditsSendTheSelectedModelWithTheReferences(t *testing.T) {
	for _, e := range gpt26 {
		t.Run(string(e.model), func(t *testing.T) {
			var gotPath string
			fields := map[string][]string{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if !assert.NoError(t, err) {
					return
				}
				mr := multipart.NewReader(r.Body, params["boundary"])
				for {
					part, err := mr.NextPart()
					if err == io.EOF {
						break
					}
					if !assert.NoError(t, err) {
						return
					}
					data, _ := io.ReadAll(part)
					if part.FileName() == "" {
						fields[part.FormName()] = append(fields[part.FormName()], string(data))
					}
				}
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("EDITED")) + `"}]}`))
			}))
			defer srv.Close()
			b := backendsFor(t, srv, memKeys{"openai": "sk-test"})

			_, err := b.Image[e.model].GenerateImage(context.Background(), e.apiID, modeladapter.ImageRequest{
				Prompt: "recolor", AspectRatio: "1:1", Inputs: []modeladapter.Input{{Data: []byte("ONE"), MimeType: "image/png"}},
			})

			require.NoError(t, err)
			assert.Equal(t, "/v1/images/edits", gotPath)
			assert.Equal(t, map[string][]string{"model": {e.apiID}, "prompt": {"recolor"}, "size": {"1024x1024"}, "quality": {"medium"}}, fields)
		})
	}
}

func TestGPTImage26ActualCostIsComputedFromTheUsageAtTheTokenPrices(t *testing.T) {
	for _, e := range gpt26 {
		t.Run(string(e.model), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("IMG")) + `"}],
					"usage":{"total_tokens":1368,"input_tokens":68,"output_tokens":1300,"input_tokens_details":{"text_tokens":60,"image_tokens":8}}}`))
			}))
			defer srv.Close()
			b := backendsFor(t, srv, memKeys{"openai": "sk-test"})

			res, err := b.Image[e.model].GenerateImage(context.Background(), e.apiID, modeladapter.ImageRequest{Prompt: "a bottle"})

			require.NoError(t, err)
			require.NotNil(t, res.ActualCost)
			assert.InDelta(t, (60*5.0+8*8.0+1300*30.0)/1e6, *res.ActualCost, 1e-12, "$5 text, $8 image input, $30 image output per million tokens")
		})
	}
}

func TestAPriceTheUserSetIsTheEstimateOfGPTImage26(t *testing.T) {
	cfg := loadModels(t, `{"models":{"gpt-image-2.6-flare":{"cost_per_image_usd":0.07}}}`)

	assert.InDelta(t, 0.07, cfg.ImageCost(domain.ModelGPTImage26Flare), 1e-9)
	assert.InDelta(t, 0.07, cfg.ImageCostFor(domain.ModelGPTImage26Flare, "16:9"), 1e-9)
	assert.Zero(t, cfg.ImageCost(domain.ModelGPTImage26Sunburst), "the other model keeps its own (unknown) price")
}

// The subscription's models are separate identifiers: the one of a result says which model drew it, and each asks the Codex
// endpoint for its own API model.
func TestEverySubscriptionModelHasItsOwnSettingsAndTheAPIModelOfItsPair(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	seen := map[domain.ModelName]bool{}
	for _, e := range chatgptid.Entries() {
		assert.False(t, seen[e.Model], "%s is listed once", e.Model)
		seen[e.Model] = true
		assert.True(t, chatgptid.Is(e.Model))
		assert.Equal(t, e.APIModel, cfg.Models[e.Model].Model, "%s asks for %s", e.Model, e.APIModel)
		assert.Equal(t, modeladapter.Vendor(chatgptid.Vendor), modeladapter.VendorOf(e.Model))
		assert.True(t, modeladapter.AcceptsImageInputs(e.Model))
		assert.NotEmpty(t, e.Label)
	}
	assert.Len(t, seen, 3, "GPT Image 2, 2.6 Flare and 2.6 Sunburst")
	assert.False(t, chatgptid.Is(domain.ModelGPTImage26Flare), "the API model is not the subscription's")
}
