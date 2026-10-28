package preparetest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The models added after the first three (spec/architecture/model-adapters.md §4, "追加の動画モデル"): they can be called
// by their identifier, but the default Quality Policies and Multi-model routing candidates do not use them.

var (
	omniTarget   = modeladapter.VideoTarget{Model: domain.ModelGeminiOmniFlash, Resolution: "720p"}
	kling4Target = modeladapter.VideoTarget{Model: domain.ModelKling4, Resolution: "1080p"}
	seed25Target = modeladapter.VideoTarget{Model: domain.ModelSeedance25, Resolution: "720p"}
)

func TestAddedModelsAreVideoModelsOfTheirVendorWithAPriceAndNoDefaultRoute(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	for _, tt := range []struct {
		model  domain.ModelName
		vendor modeladapter.Vendor
		apiID  string
	}{
		{domain.ModelGeminiOmniFlash, modeladapter.VendorGoogle, "gemini-omni-1.1-flash"},
		{domain.ModelKling4, modeladapter.VendorKling4, "kling-4.0"},
		{domain.ModelSeedance25, modeladapter.VendorSeedance, "dreamina-seedance-2-5-260628"},
		{domain.ModelMiniMaxH3, modeladapter.VendorMiniMax, "MiniMax-H3"},
		{domain.ModelMiniMaxH3Max, modeladapter.VendorMiniMax, "MiniMax-H3-Max"},
		{domain.ModelWan3, modeladapter.VendorAlibaba, "wan3.0-video"},
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

func TestKling4IsPricedFromTheOfficialUnitPrice(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	// 1.0 unit per second at 720p and 1.5 at 1080p (no video input), 1 unit = USD 0.14 (list price).
	assert.InDelta(t, 0.14*10, cfg.VideoCost(modeladapter.VideoTarget{Model: domain.ModelKling4, Resolution: "720p"}, 10), 1e-9)
	assert.InDelta(t, 0.14*1.5*10, cfg.VideoCost(kling4Target, 10), 1e-9)
}

func TestKling4HasItsOwnVendorOnTheNewHostWhileKling3KeepsTheOldOne(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Equal(t, "https://api-singapore.klingai.com", cfg.Vendors[modeladapter.VendorKling4].BaseURL)
	assert.Equal(t, "https://api.klingai.com", cfg.Vendors[modeladapter.VendorKling].BaseURL, "Kling 3.0 is not moved to the new host: that needs a real call")
	assert.Equal(t, "kling-v3", cfg.ModelSettings(domain.ModelKling3).Model)
	assert.Equal(t, modeladapter.VendorKling, modeladapter.VendorOf(domain.ModelKling3))
	assert.Contains(t, modeladapter.Vendors(), modeladapter.VendorKling4)
}

func TestPrepareForKling4TakesAnyWholeSecondFrom3To30AndFoldsTheAvoidList(t *testing.T) {
	caps, ok := modeladapter.Capabilities(domain.ModelKling4)
	require.True(t, ok)
	assert.Equal(t, []string{"720p", "1080p"}, caps.Resolutions)
	assert.Len(t, caps.Durations, 28)
	assert.Equal(t, 3.0, caps.SnapDuration(1))
	assert.Equal(t, 17.0, caps.SnapDuration(16.4))
	assert.Equal(t, 30.0, caps.SnapDuration(45))
	assert.Zero(t, caps.MaxReferenceImages)

	job := fullJob()
	job.DurationSeconds = 25
	req, err := modeladapter.Prepare(kling4Target, job)
	require.NoError(t, err)
	assert.Equal(t, 25.0, req.DurationSeconds)
	assert.Equal(t, "1080p", req.Resolution)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Empty(t, req.References, "Kling 4.0 takes frames here, not free references")
	assert.Empty(t, req.NegativePrompt, "the prompt takes negative descriptions: there is no field")
	assert.Contains(t, req.Prompt, "Avoid: blurry")
}

func TestPrepareForKling4AllowsALongPromptThanKling3(t *testing.T) {
	job := fullJob()
	job.Spec.ShotObjective = strings.Repeat("A very long objective. ", 400)
	job.Spec.Constraints = []string{"preserve_logo"}

	req, err := modeladapter.Prepare(kling4Target, job)
	require.NoError(t, err)
	assert.Greater(t, len([]rune(req.Prompt)), 2400, "the limit is 26,000 characters of English")
	assert.LessOrEqual(t, len([]rune(req.Prompt)), 26000)
	assert.Contains(t, req.Prompt, "keep the logo and label text sharp and undistorted")
	assert.Contains(t, req.Prompt, "Avoid:", "the avoid list survives the cut")

	kling3, err := modeladapter.Prepare(klingTarget, job)
	require.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(kling3.Prompt)), 2400, "Kling 3.0 is unchanged")
}

func TestSeedance25IsPricedFromTheOfficialBillingExamples(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	// ModelArk's billing examples for a 16:9 clip of 5 s without video input: USD 0.514 at 480p, 1.156 at 720p, 2.843 at 1080p.
	for res, perClip := range map[string]float64{"480p": 0.514, "720p": 1.156, "1080p": 2.843} {
		assert.InDelta(t, perClip, cfg.VideoCost(modeladapter.VideoTarget{Model: domain.ModelSeedance25, Resolution: res}, 5), 0.01, res)
	}
	assert.Equal(t, "seedance-2-0", cfg.ModelSettings(domain.ModelSeedance2).Model, "Seedance 2.0 keeps the model identifier it has")
	assert.Equal(t, cfg.Vendors[modeladapter.VendorSeedance].BaseURL, "https://ark.ap-southeast.bytepluses.com")
}

func TestPrepareForSeedance25TakesUpTo30SecondsAnd30ReferenceImagesButNeverBoth(t *testing.T) {
	caps, ok := modeladapter.Capabilities(domain.ModelSeedance25)
	require.True(t, ok)
	assert.Equal(t, []string{"480p", "720p", "1080p"}, caps.Resolutions)
	assert.Len(t, caps.Durations, 27)
	assert.Equal(t, 4.0, caps.SnapDuration(1), "4 to 30 seconds")
	assert.Equal(t, 30.0, caps.SnapDuration(99))
	assert.True(t, caps.NativeAudio)

	job := fullJob()
	job.DurationSeconds = 21.5
	req, err := modeladapter.Prepare(seed25Target, job)
	require.NoError(t, err)
	assert.Equal(t, 22.0, req.DurationSeconds)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Empty(t, req.References, "a first frame and reference images are exclusive scenarios")

	job.FirstFrame, job.LastFrame = nil, nil
	for len(job.References) < 40 {
		job.References = append(job.References, job.References[0])
	}
	req, err = modeladapter.Prepare(seed25Target, job)
	require.NoError(t, err)
	assert.Len(t, req.References, 30, "up to 30 images of the 50 reference assets")
	assert.Empty(t, req.NegativePrompt)
	assert.Contains(t, req.Prompt, "Avoid: blurry")

	older, err := modeladapter.Prepare(seedanceTarget, job)
	require.NoError(t, err)
	assert.Len(t, older.References, 9, "Seedance 2.0 is unchanged")
	assert.LessOrEqual(t, older.DurationSeconds, 12.0)
}
