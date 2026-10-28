package preparetest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var wan3Target = modeladapter.VideoTarget{Model: domain.ModelWan3, Resolution: "720p"}

func TestWan3IsPricedFromTheSingaporeTable(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Equal(t, "wan3.0-video", cfg.ModelSettings(domain.ModelWan3).Model)
	// Singapore, per second of generated video: 480P 0.05, 720P 0.10, 1080P 0.20 USD.
	assert.InDelta(t, 0.05*10, cfg.VideoCost(modeladapter.VideoTarget{Model: domain.ModelWan3, Resolution: "480p"}, 10), 1e-9)
	assert.InDelta(t, 0.10*10, cfg.VideoCost(wan3Target, 10), 1e-9)
	assert.InDelta(t, 0.20*10, cfg.VideoCost(modeladapter.VideoTarget{Model: domain.ModelWan3, Resolution: "1080p"}, 10), 1e-9)
}

func TestWan3HasItsOwnVendorOnTheSingaporeWorkspaceHost(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Equal(t, modeladapter.VendorAlibaba, modeladapter.VendorOf(domain.ModelWan3))
	assert.Equal(t, "alibaba", string(modeladapter.VendorAlibaba), "the credential.Store key")
	assert.Equal(t, "https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com", cfg.Vendors[modeladapter.VendorAlibaba].BaseURL)
	assert.Contains(t, modeladapter.Vendors(), modeladapter.VendorAlibaba)
	assert.Positive(t, cfg.Vendors[modeladapter.VendorAlibaba].MaxConcurrency)
	assert.Greater(t, cfg.Vendors[modeladapter.VendorAlibaba].TimeoutSeconds, 900, "longer than the other cloud video vendors: up to 30 seconds of video")
}

func TestWan3CapabilitiesFollowTheAPIReference(t *testing.T) {
	caps, ok := modeladapter.Capabilities(domain.ModelWan3)
	require.True(t, ok)
	assert.Equal(t, []string{"480p", "720p", "1080p"}, caps.Resolutions)
	assert.Len(t, caps.Durations, 29)
	assert.Equal(t, 2.0, caps.SnapDuration(1), "2 to 30 seconds without video input")
	assert.Equal(t, 30.0, caps.SnapDuration(99))
	assert.Equal(t, 10, caps.MaxReferenceImages)
	assert.True(t, caps.FirstFrame)
	assert.True(t, caps.LastFrame)
	assert.True(t, caps.NativeAudio)
	assert.False(t, modeladapter.SupportsSeed(domain.ModelWan3))
}

func TestPrepareForWan3KeepsTheFramesAndFoldsTheAvoidListIntoThePrompt(t *testing.T) {
	job := fullJob()
	job.DurationSeconds = 17.2

	req, err := modeladapter.Prepare(wan3Target, job)

	require.NoError(t, err)
	assert.Equal(t, 18.0, req.DurationSeconds)
	assert.Equal(t, "720p", req.Resolution)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Empty(t, req.References, "first / last frame and reference images are mutually exclusive: the frames win")
	assert.Empty(t, req.NegativePrompt)
	assert.Contains(t, req.Prompt, "Avoid: blurry")
}

func TestPrepareForWan3KeepsUpTo10ReferenceImagesWhenTheJobPrefersThem(t *testing.T) {
	job := fullJob()
	job.PreferReferences = true
	for len(job.References) < 14 {
		job.References = append(job.References, job.References[0])
	}

	req, err := modeladapter.Prepare(wan3Target, job)

	require.NoError(t, err)
	assert.Len(t, req.References, 10)
	assert.Nil(t, req.FirstFrame)
	assert.Nil(t, req.LastFrame)
}

func TestPrepareForWan3CutsThePromptAt20000Characters(t *testing.T) {
	job := fullJob()
	job.Spec.ShotObjective = strings.Repeat("A very long objective. ", 1500)
	job.Spec.Constraints = []string{"preserve_logo"}

	req, err := modeladapter.Prepare(wan3Target, job)

	require.NoError(t, err)
	assert.Greater(t, len([]rune(req.Prompt)), 7000, "longer than the other models' limits")
	assert.LessOrEqual(t, len([]rune(req.Prompt)), 20000)
	assert.Contains(t, req.Prompt, "keep the logo and label text sharp and undistorted")
	assert.Contains(t, req.Prompt, "Avoid:", "the API cuts what is over the limit: the Constraints and the avoid list are kept whole")
}

func TestWan3IsNotInTheDefaultRoutes(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Contains(t, domain.VideoModels(), domain.ModelWan3)
	for _, p := range cfg.Policies {
		assert.NotEqual(t, domain.ModelWan3, p.Primary.Model)
		assert.NotEqual(t, domain.ModelWan3, p.Fallback.Model)
	}
	for _, targets := range cfg.Routing.Candidates {
		for _, c := range targets {
			assert.NotEqual(t, domain.ModelWan3, c.Model)
		}
	}
}
