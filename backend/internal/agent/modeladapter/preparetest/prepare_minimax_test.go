package preparetest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

var (
	minimaxH3Target    = modeladapter.VideoTarget{Model: domain.ModelMiniMaxH3, Resolution: "768p"}
	minimaxH3MaxTarget = modeladapter.VideoTarget{Model: domain.ModelMiniMaxH3Max, Resolution: "480p"}
)

func TestMiniMaxIsPricedFromTheOfficialPerSecondPrices(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	// Pay-as-you-go: output video per second.
	assert.InDelta(t, 0.08*10, cfg.VideoCost(minimaxH3Target, 10), 1e-9)
	assert.InDelta(t, 0.13*10, cfg.VideoCost(modeladapter.VideoTarget{Model: domain.ModelMiniMaxH3, Resolution: "2k"}, 10), 1e-9)
	assert.InDelta(t, 0.05*10, cfg.VideoCost(minimaxH3MaxTarget, 10), 1e-9)
	assert.InDelta(t, 0.08*10, cfg.VideoCost(modeladapter.VideoTarget{Model: domain.ModelMiniMaxH3Max, Resolution: "768p"}, 10), 1e-9)
}

func TestMiniMaxHasItsOwnVendorOnTheGlobalHost(t *testing.T) {
	cfg := modeladapter.DefaultConfig()
	assert.Equal(t, "https://api.minimax.io", cfg.Vendors[modeladapter.VendorMiniMax].BaseURL)
	assert.Equal(t, modeladapter.VendorMiniMax, modeladapter.VendorOf(domain.ModelMiniMaxH3))
	assert.Equal(t, modeladapter.VendorMiniMax, modeladapter.VendorOf(domain.ModelMiniMaxH3Max))
	assert.Equal(t, "minimax", string(modeladapter.VendorMiniMax), "the credential.Store key")
	assert.Contains(t, modeladapter.Vendors(), modeladapter.VendorMiniMax)
	assert.Positive(t, cfg.Vendors[modeladapter.VendorMiniMax].MaxConcurrency)
	assert.GreaterOrEqual(t, cfg.Vendors[modeladapter.VendorMiniMax].TimeoutSeconds, 900)
}

func TestMiniMaxCapabilitiesFollowTheOfficialSpecs(t *testing.T) {
	h3, ok := modeladapter.Capabilities(domain.ModelMiniMaxH3)
	require.True(t, ok)
	assert.Equal(t, []string{"768p", "2k"}, h3.Resolutions)
	assert.Len(t, h3.Durations, 12)
	assert.Equal(t, 4.0, h3.SnapDuration(1), "H3 makes 4 to 15 seconds")
	assert.Equal(t, 15.0, h3.SnapDuration(60))
	assert.True(t, h3.FirstFrame)
	assert.True(t, h3.LastFrame)

	h3max, ok := modeladapter.Capabilities(domain.ModelMiniMaxH3Max)
	require.True(t, ok)
	assert.Equal(t, []string{"480p", "768p"}, h3max.Resolutions)
	assert.Equal(t, 5.0, h3max.SnapDuration(1), "H3 Max does not make 4 seconds")
	assert.Equal(t, 15.0, h3max.SnapDuration(60))
}

func TestPrepareForMiniMaxSnapsTheLengthAndFoldsTheAvoidListIntoThePrompt(t *testing.T) {
	job := fullJob()
	job.DurationSeconds = 6.2

	req, err := modeladapter.Prepare(minimaxH3Target, job)

	require.NoError(t, err)
	assert.Equal(t, 7.0, req.DurationSeconds)
	assert.Equal(t, "768p", req.Resolution)
	assert.NotNil(t, req.FirstFrame)
	assert.NotNil(t, req.LastFrame)
	assert.Empty(t, req.References, "first / last frame and reference images are exclusive modes: the frames win")
	assert.Empty(t, req.NegativePrompt, "there is no negative prompt field")
	assert.Contains(t, req.Prompt, "Avoid: blurry")

	short := fullJob()
	short.DurationSeconds = 4
	req, err = modeladapter.Prepare(minimaxH3MaxTarget, short)
	require.NoError(t, err)
	assert.Equal(t, 5.0, req.DurationSeconds)
}

func TestPrepareForMiniMaxKeepsTheReferenceImagesWithinTheFreeQuotaWhenTheJobPrefersThem(t *testing.T) {
	job := fullJob()
	job.PreferReferences = true

	h3, err := modeladapter.Prepare(minimaxH3Target, job)
	require.NoError(t, err)
	assert.Len(t, h3.References, 5, "the first 5 images of a task are free")
	assert.Nil(t, h3.FirstFrame)
	assert.Nil(t, h3.LastFrame)

	h3max, err := modeladapter.Prepare(minimaxH3MaxTarget, job)
	require.NoError(t, err)
	assert.Len(t, h3max.References, 2, "H3 Max: the first 2 images of a task are free")
}

func TestPrepareForMiniMaxCutsThePromptAt7000Characters(t *testing.T) {
	job := fullJob()
	job.Spec.ShotObjective = strings.Repeat("A very long objective. ", 800)
	job.Spec.Constraints = []string{"preserve_logo"}

	req, err := modeladapter.Prepare(minimaxH3Target, job)

	require.NoError(t, err)
	assert.Greater(t, len([]rune(req.Prompt)), 2400, "longer than the 2400 characters of the other models")
	assert.LessOrEqual(t, len([]rune(req.Prompt)), 7000)
	assert.Contains(t, req.Prompt, "keep the logo and label text sharp and undistorted")
	assert.Contains(t, req.Prompt, "Avoid:", "the avoid list survives the cut")
}
