package videoenhance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
)

// adultSource marks the model that made aVideo as an adult model of the catalog.
func adultSource() gentesttest.Option {
	return gentesttest.WithCatalogInfo(func(id string) (modelfilter.CatalogInfo, bool) {
		return modelfilter.CatalogInfo{Adult: id == string(domain.ModelKling3)}, true
	})
}

// The result of an adult model may go to Topaz (Issue #1031, decision of 2026-11-05), but only after the user read the warning and confirmed it.
func TestAnAdultResultGoesToTheCloudOnlyAfterTheWarningIsConfirmed(t *testing.T) {
	cloud := newCloud(true)
	r := newRig(t, gentesttest.WithCloud(cloud), adultSource())
	src := aVideo(t, r)

	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))
	require.NoError(t, err, "the quote is not refused: sending is allowed")
	assert.True(t, est.Adult)
	assert.Equal(t, venhance.AdultWarning, est.AdultWarning)
	assert.Contains(t, est.AdultWarning, "Topaz Labs", "tells that the content goes to the external cloud")
	assert.Contains(t, est.AdultWarning, "禁止", "tells that what the terms of Topaz forbid is not to be sent")
	assert.Contains(t, est.AdultWarning, "規約に従", "tells that the output follows the terms of Topaz")
	assert.Contains(t, est.Message, "adult_confirmed")

	req := upscale(src, proteus)
	req.EstimateID, req.ConfirmedCredits = est.EstimateID, *est.Credits
	_, err = videoenhance.Enhance(ctx, r.Svc, req)
	assert.Contains(t, invalidMessage(t, err), venhance.AdultWarning, "the credits are confirmed but the warning is not")
	assert.Empty(t, cloud.Runs, "nothing is sent without the confirmation")

	req.AdultConfirmed = true // the same quote still stands: the refusal did not use it up
	res := enhance(t, r, req)

	require.Equal(t, "succeeded", res.Status, res.Error)
	require.Len(t, cloud.Runs, 1)
	assert.True(t, res.VideoEnhance.Adult)
}

// A result that is not adult is not warned and is not asked: nothing changes for it.
func TestAResultThatIsNotAdultHasNoWarning(t *testing.T) {
	cloud := newCloud(true)
	r := newRig(t, gentesttest.WithCloud(cloud), gentesttest.WithCatalogInfo(func(string) (modelfilter.CatalogInfo, bool) { return modelfilter.CatalogInfo{}, true }))
	src := aVideo(t, r)

	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))
	require.NoError(t, err)
	assert.False(t, est.Adult)
	assert.Empty(t, est.AdultWarning)
	assert.NotContains(t, est.Message, "adult_confirmed")

	req := upscale(src, proteus)
	req.EstimateID, req.ConfirmedCredits = est.EstimateID, *est.Credits
	res := enhance(t, r, req)

	require.Equal(t, "succeeded", res.Status, res.Error)
	assert.False(t, res.VideoEnhance.Adult)
}

// A local model sends nothing out, so an adult result is not warned for it.
func TestAnAdultResultHasNoWarningForALocalModel(t *testing.T) {
	r := newRig(t, adultSource())
	src := aVideo(t, r)

	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, string(gentesttest.UpscaleVideoModel)))
	require.NoError(t, err)
	assert.Empty(t, est.AdultWarning)
	res := enhance(t, r, upscale(src, string(gentesttest.UpscaleVideoModel)))
	require.Equal(t, "succeeded", res.Status, res.Error)
	assert.True(t, res.VideoEnhance.Adult, "remembered, so that sending the output on to the cloud asks again")
}

// The result of an enhancement of an adult video is adult too: sending it on to the cloud asks again.
func TestTheEnhancedResultOfAnAdultVideoStaysAdult(t *testing.T) {
	cloud := newCloud(true)
	r := newRig(t, gentesttest.WithCloud(cloud), adultSource())
	local := enhance(t, r, upscale(aVideo(t, r), string(gentesttest.UpscaleVideoModel)))
	require.Equal(t, "succeeded", local.Status, local.Error)

	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(local.ID, proteus))
	require.NoError(t, err)
	assert.True(t, est.Adult)
	req := upscale(local.ID, proteus)
	req.EstimateID, req.ConfirmedCredits = est.EstimateID, *est.Credits
	_, err = videoenhance.Enhance(ctx, r.Svc, req)
	assert.Contains(t, invalidMessage(t, err), venhance.AdultWarning)
	assert.Empty(t, cloud.Runs)
}
