package preparetest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The models added after the first three (spec/architecture/model-adapters.md §4, "追加の動画モデル"): they can be called
// by their identifier, but the default Quality Policies and Multi-model routing candidates do not use them.

var omniTarget = modeladapter.VideoTarget{Model: domain.ModelGeminiOmniFlash, Resolution: "720p"}

func TestAddedModelsAreVideoModelsOfTheirVendorWithAPriceAndNoDefaultRoute(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	for _, tt := range []struct {
		model  domain.ModelName
		vendor modeladapter.Vendor
		apiID  string
	}{
		{domain.ModelGeminiOmniFlash, modeladapter.VendorGoogle, "gemini-omni-1.1-flash"},
	} {
		t.Run(string(tt.model), func(t *testing.T) {
			assert.Contains(t, domain.VideoModels(), tt.model)
			assert.Equal(t, tt.vendor, modeladapter.VendorOf(tt.model))
			assert.Equal(t, tt.apiID, cfg.ModelSettings(tt.model).Model)
			caps, ok := modeladapter.Capabilities(tt.model)
			require.True(t, ok)
			for _, res := range caps.Resolutions {
				assert.Positive(t, cfg.VideoCost(modeladapter.VideoTarget{Model: tt.model, Resolution: res}, 1), "every offered resolution has an official price: %s", res)
			}
			assert.False(t, modeladapter.SupportsSeed(tt.model))
			for _, p := range cfg.Policies {
				assert.NotEqual(t, tt.model, p.Primary.Model)
				assert.NotEqual(t, tt.model, p.Fallback.Model)
			}
			for _, targets := range cfg.Routing.Candidates {
				for _, c := range targets {
					assert.NotEqual(t, tt.model, c.Model)
				}
			}
		})
	}
}

func TestGeminiOmniFlashIsPricedFromTheOfficialTokenRate(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	// 5,792 tokens per second of 720p video at USD 17.50 per million output tokens is 0.1014 USD; Google states "approximately $0.10".
	assert.InDelta(t, 0.5, cfg.VideoCost(omniTarget, 5), 1e-9)
	assert.InDelta(t, 5792*17.5/1e6, cfg.Models[domain.ModelGeminiOmniFlash].CostPerSecond["720p"], 0.002)
}

func TestPrepareForGeminiOmniFlashSnapsToTheClipLengthsAndKeepsOneKindOfImage(t *testing.T) {
	caps, ok := modeladapter.Capabilities(domain.ModelGeminiOmniFlash)
	require.True(t, ok)
	assert.Equal(t, []string{"720p"}, caps.Resolutions)
	assert.True(t, caps.NativeAudio)
	assert.Equal(t, 3.0, caps.SnapDuration(1), "clips are 3 to 10 seconds")
	assert.Equal(t, 10.0, caps.SnapDuration(30))

	job := fullJob()
	job.DurationSeconds = 6.2
	req, err := modeladapter.Prepare(omniTarget, job)
	require.NoError(t, err)
	assert.Equal(t, 7.0, req.DurationSeconds)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Empty(t, req.References, "a model reads either the frames or the references")
	assert.Empty(t, req.NegativePrompt, "Omni has no negative prompt: the avoid list is part of the prompt")
	assert.Contains(t, req.Prompt, "Avoid: blurry")
	assert.Contains(t, req.Prompt, "Duration 7 seconds", "the length is asked for in the prompt: there is no length parameter")
	assert.False(t, cjk.MatchString(req.Prompt))

	job.FirstFrame, job.LastFrame = nil, nil
	req, err = modeladapter.Prepare(omniTarget, job)
	require.NoError(t, err)
	assert.Len(t, req.References, 6)
}
