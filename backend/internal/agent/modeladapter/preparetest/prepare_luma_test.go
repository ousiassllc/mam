package preparetest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var lumaTarget = modeladapter.VideoTarget{Model: domain.ModelLumaRay32, Resolution: "720p"}

func lumaTargetAt(res string) modeladapter.VideoTarget {
	return modeladapter.VideoTarget{Model: domain.ModelLumaRay32, Resolution: res}
}

// Pricing: credits per 5 s (360p 20 / 540p 50 / 720p 100 / 1080p 400), 10 s three times that; 1 credit = 0.003 USD (list rate).
func TestLumaIsPricedFromTheCreditTableAtTheListRate(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	const rate = 0.003
	for res, credits := range map[string][2]float64{"360p": {20, 60}, "540p": {50, 150}, "720p": {100, 300}, "1080p": {400, 1200}} {
		assert.InDelta(t, credits[0]*rate, cfg.VideoCost(lumaTargetAt(res), 5), 1e-9, "5 s at %s", res)
		assert.InDelta(t, credits[1]*rate, cfg.VideoCost(lumaTargetAt(res), 10), 1e-9, "10 s at %s", res)
	}
}

func TestLumaHasItsOwnVendorOnTheAgentsHost(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Equal(t, modeladapter.VendorLuma, modeladapter.VendorOf(domain.ModelLumaRay32))
	assert.Equal(t, "luma", string(modeladapter.VendorLuma), "the credential.Store key")
	assert.Equal(t, "https://agents.lumalabs.ai", cfg.Vendors[modeladapter.VendorLuma].BaseURL)
	assert.Contains(t, modeladapter.Vendors(), modeladapter.VendorLuma)
	assert.Positive(t, cfg.Vendors[modeladapter.VendorLuma].MaxConcurrency)
	assert.Positive(t, cfg.Vendors[modeladapter.VendorLuma].TimeoutSeconds)
	assert.Equal(t, "ray-3.2", cfg.ModelSettings(domain.ModelLumaRay32).Model)
}

func TestLumaCapabilitiesFollowTheOfficialSpecification(t *testing.T) {
	caps, ok := modeladapter.Capabilities(domain.ModelLumaRay32)
	require.True(t, ok)
	assert.Equal(t, []float64{5, 10}, caps.Durations)
	assert.Equal(t, []string{"360p", "540p", "720p", "1080p"}, caps.Resolutions)
	assert.Zero(t, caps.MaxReferenceImages)
	assert.True(t, caps.FirstFrame)
	assert.True(t, caps.LastFrame)
	assert.Equal(t, 5.0, caps.SnapDuration(3), "the length is rounded up to 5 or 10")
	assert.Equal(t, 10.0, caps.SnapDuration(6))
	assert.Equal(t, 10.0, caps.SnapDuration(99))
	assert.False(t, modeladapter.SupportsSeed(domain.ModelLumaRay32))
}

func TestPrepareForLumaWithFramesKeepsToFiveSecondsBecauseTenSecondsRefusesFrames(t *testing.T) {
	job := fullJob()
	job.DurationSeconds = 8

	req, err := modeladapter.Prepare(lumaTarget, job)

	require.NoError(t, err)
	assert.Equal(t, 5.0, req.DurationSeconds)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Empty(t, req.References)
	assert.Empty(t, req.NegativePrompt)
	assert.Contains(t, req.Prompt, "Avoid: blurry")
}

func TestPrepareForLumaWithoutFramesRoundsTheLengthTo5Or10(t *testing.T) {
	job := fullJob()
	job.FirstFrame, job.LastFrame = nil, nil

	for seconds, want := range map[float64]float64{2: 5, 5: 5, 5.5: 10, 8: 10, 12: 10} {
		job.DurationSeconds = seconds
		req, err := modeladapter.Prepare(lumaTarget, job)
		require.NoError(t, err)
		assert.Equal(t, want, req.DurationSeconds, "%v s", seconds)
		assert.Empty(t, req.References, "the video type has no free reference images")
	}
}

func TestPrepareForLumaCutsThePromptAt6000CharactersKeepingTheConstraints(t *testing.T) {
	job := fullJob()
	job.Spec.ShotObjective = strings.Repeat("A very long objective. ", 800)
	job.Spec.Constraints = []string{"preserve_logo"}

	req, err := modeladapter.Prepare(lumaTarget, job)

	require.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(req.Prompt)), 6000)
	assert.Contains(t, req.Prompt, "keep the logo and label text sharp and undistorted")
	assert.Contains(t, req.Prompt, "Avoid:")
}

func TestLumaIsNotInTheDefaultRoutes(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Contains(t, domain.VideoModels(), domain.ModelLumaRay32)
	for _, p := range cfg.Policies {
		assert.NotEqual(t, domain.ModelLumaRay32, p.Primary.Model)
		assert.NotEqual(t, domain.ModelLumaRay32, p.Fallback.Model)
	}
	for _, targets := range cfg.Routing.Candidates {
		for _, c := range targets {
			assert.NotEqual(t, domain.ModelLumaRay32, c.Model)
		}
	}
}
