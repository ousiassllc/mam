package videoenhance_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
)

func TestACloudModelWithoutTheKeyIsNotRun(t *testing.T) {
	cloud := newCloud(false)
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)

	_, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))
	assert.Contains(t, invalidMessage(t, err), "API キー")
	_, err = videoenhance.Enhance(ctx, r.Svc, upscale(src, proteus))
	assert.Contains(t, invalidMessage(t, err), "API キー")
	assert.Empty(t, cloud.Estimates)
	assert.Empty(t, cloud.Runs)
}

// A cloud model costs credits: it does not run without the quote and the credits the user confirmed.
func TestACloudEnhancementRunsOnlyWithTheConfirmedQuote(t *testing.T) {
	cloud := newCloud(true)
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)

	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))
	require.NoError(t, err)
	require.NotNil(t, est.Credits)
	assert.InDelta(t, 12.5, *est.Credits, 0)
	assert.InDelta(t, 10, *est.CreditsMin, 0)
	assert.True(t, est.Paid)
	assert.NotEmpty(t, est.EstimateID)
	assert.Equal(t, "Powered by Topaz Labs", est.Attribution)
	require.Len(t, cloud.Estimates, 1)
	assert.Equal(t, "prob-4", cloud.Estimates[0].Model, "the provider's own name")
	assert.Equal(t, cloudenhance.Source{Container: "mp4", SizeBytes: 7, Duration: 5.0625, FrameCount: 81, FPS: 16, Width: 480, Height: 848, HasAudio: true}, cloud.Estimates[0].Source)

	// without the quote
	_, err = videoenhance.Enhance(ctx, r.Svc, upscale(src, proteus))
	assert.Contains(t, invalidMessage(t, err), "見積り")
	// with a quote nobody gave
	req := upscale(src, proteus)
	req.EstimateID, req.ConfirmedCredits = "ve-unknown", 12.5
	_, err = videoenhance.Enhance(ctx, r.Svc, req)
	assert.Contains(t, conflictMessage(t, err), "見積りの有効期限が切れました。もう一度見積りを取ってください")
	// with other credits than the quote
	req.EstimateID, req.ConfirmedCredits = est.EstimateID, 1
	_, err = videoenhance.Enhance(ctx, r.Svc, req)
	assert.Contains(t, conflictMessage(t, err), "見積り")
	// with the quote of other values
	other := upscale(src, proteus)
	other.Scale, other.EstimateID, other.ConfirmedCredits = 3, est.EstimateID, 12.5
	_, err = videoenhance.Enhance(ctx, r.Svc, other)
	assert.Contains(t, conflictMessage(t, err), "条件")
	assert.Empty(t, cloud.Runs, "nothing was sent while it was refused")

	// confirmed
	req.ConfirmedCredits = *est.Credits
	res := enhance(t, r, req)

	require.Equal(t, "succeeded", res.Status, res.Error)
	require.Len(t, cloud.Runs, 1)
	assert.Equal(t, "req-1", cloud.Runs[0].RequestID, "exactly what was quoted")
	assert.Equal(t, "MP4DATA", string(cloud.Sources[0]), "the file of the source is what is uploaded")
	assert.Equal(t, "topaz", res.VideoEnhance.Provider)
	assert.Equal(t, proteus, res.VideoEnhance.Model)
	assert.Equal(t, src, res.VideoEnhance.SourceResultID)
	require.NotNil(t, res.VideoEnhance.Topaz)
	assert.Equal(t, record.TopazRun{RequestID: "req-1", EstimatedCredits: 12.5, Attribution: "Powered by Topaz Labs"}, *res.VideoEnhance.Topaz)
	assert.Equal(t, modelcatalog.BasisUnknown, res.CostBasis, "credits are not dollars")
	assert.Nil(t, res.EstimatedUSD)
	file, err := r.Svc.ResultFile(ctx, res.ID)
	require.NoError(t, err)
	data, err := os.ReadFile(file.Path)
	require.NoError(t, err)
	assert.Equal(t, "TOPAZ", string(data))
	entries, err := os.ReadDir(filepath.Dir(file.Path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no half-written file is left")

	// a quote runs once
	_, err = videoenhance.Enhance(ctx, r.Svc, req)
	assert.Contains(t, conflictMessage(t, err), "有効期限")
	assert.Len(t, cloud.Runs, 1, "the credits are never reserved twice")
}

func TestACloudEstimateThatCannotBeHadMeansNothingRuns(t *testing.T) {
	cloud := newCloud(true)
	cloud.EstimateErr = errors.New("no credits in the answer")
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)

	_, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))

	require.Error(t, err)
	_, ok := apperr.As(err)
	assert.True(t, ok, "told as the common error")
	_, err = videoenhance.Enhance(ctx, r.Svc, upscale(src, proteus))
	assert.Error(t, err)
	assert.Empty(t, cloud.Runs)
}

func TestAFailedCloudRunIsNotSentAgain(t *testing.T) {
	cloud := newCloud(true)
	cloud.RunErr = apperr.New(apperr.CodeUnknown).WithDetail("failed after accept")
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)
	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, videoenhance.Request{ResultID: src, Op: cloudenhance.OpInterpolate, Model: apollo, TargetFPS: 24})
	require.NoError(t, err)

	res := enhance(t, r, videoenhance.Request{ResultID: src, Op: cloudenhance.OpInterpolate, Model: apollo, TargetFPS: 24, EstimateID: est.EstimateID, ConfirmedCredits: *est.Credits})

	assert.Equal(t, "failed", res.Status)
	assert.NotEmpty(t, res.Error)
	assert.Len(t, cloud.Runs, 1, "run once, never again")
}

// Every comparison with NaN is false: a NaN (or Inf, or negative) confirmation must not pass the gate, and nothing is sent to the provider.
func TestAConfirmationThatIsNotANumberDoesNotPassTheGate(t *testing.T) {
	cloud := newCloud(true)
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)
	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))
	require.NoError(t, err)

	for name, credits := range map[string]float64{"NaN": math.NaN(), "+Inf": math.Inf(1), "-Inf": math.Inf(-1), "negative": -12.5, "zero": 0} {
		req := upscale(src, proteus)
		req.EstimateID, req.ConfirmedCredits = est.EstimateID, credits

		_, err := videoenhance.Enhance(ctx, r.Svc, req)

		assert.Contains(t, conflictMessage(t, err), "確認したクレジット", name)
		assert.Empty(t, cloud.Runs, name)
	}
	_, ok := r.Svc.VideoEstimates.Get(est.EstimateID)
	assert.True(t, ok, "a refused confirmation does not use the quote up")
}

// The result remembers the output the quote promised (the sizes of Topaz are multiples of 4), not the plan the app made on its own.
func TestACloudResultRemembersTheSizesOfTheQuote(t *testing.T) {
	cloud := newCloud(true)
	cloud.Quote.Width, cloud.Quote.Height, cloud.Quote.FPS = 964, 1700, 16
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)
	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))
	require.NoError(t, err)
	req := upscale(src, proteus)
	req.EstimateID, req.ConfirmedCredits = est.EstimateID, *est.Credits

	res := enhance(t, r, req)

	require.Equal(t, "succeeded", res.Status, res.Error)
	assert.Equal(t, [2]int{964, 1700}, [2]int{res.VideoEnhance.Width, res.VideoEnhance.Height})
}
