package judge_test

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/judge"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// clipOf is a 3-second clip at 30 fps with one picture per 0.25 s.
func clipOf(t *testing.T, pics ...[]byte) judge.Clip {
	t.Helper()
	f := &fakeFrames{pictures: pics, duration: 0.25 * float64(len(pics)), fps: 30}
	clip, err := f.Sample(context.Background(), "", judge.SampleSpec{})
	require.NoError(t, err)
	return clip
}

// subject has the reference faces of the given similarities to the reference face (red 0).
func subject(t *testing.T, id string, sims ...float64) judge.FaceSubject {
	t.Helper()
	s := judge.FaceSubject{ID: id}
	for _, sim := range sims {
		s.ReferenceImages = append(s.ReferenceImages, facePicture(t, sim))
	}
	return s
}

func frames(t *testing.T, sims ...float64) [][]byte {
	t.Helper()
	var out [][]byte
	for _, s := range sims {
		if s < 0 {
			out = append(out, noFacePicture(t))
		} else {
			out = append(out, facePicture(t, s))
		}
	}
	return out
}

func measure(t *testing.T, subjects []judge.FaceSubject, clip judge.Clip) judge.FaceReport {
	t.Helper()
	m := judge.NewFaceMeasurer(&fakeEngine{}, judge.DefaultConfig().Face)
	rep, err := m.Measure(context.Background(), subjects, clip)
	require.NoError(t, err)
	return rep
}

func TestFaceMeasure_MetricsIgnoreMissingFramesAndSeparateTheFailRun(t *testing.T) {
	// The two references are the same face: thresholds calibrate to warn 0.85 and fail 0.65.
	clip := clipOf(t, frames(t, 0.95, 0.95, 0.95, 0.95, 0.2, 0.2, 0.2, 0.95, -1, -1, 0.95, 0.95)...)
	rep := measure(t, []judge.FaceSubject{subject(t, "char-a", 1.0, 1.0)}, clip)

	require.Len(t, rep.Metrics, 1)
	m := rep.Metrics[0]
	assert.Equal(t, "char-a", m.CharacterID)
	assert.Equal(t, 12, m.SampledFrames)
	assert.InDelta(t, 2.0/12, m.UndetectedRatio, 1e-3, "two frames without a face")
	assert.InDelta(t, 0.2, m.SimilarityMin, 0.02, "missing frames are not zeros")
	assert.Greater(t, m.SimilarityMean, 0.7)
	assert.True(t, m.Evaluated)
	assert.True(t, m.Calibrated, "two reference images allow the calibration")
	assert.InDelta(t, 0.85, m.WarnThreshold, 0.01)
	assert.InDelta(t, 0.65, m.FailThreshold, 0.01)

	require.Len(t, rep.Issues, 1, "one run of three frames below the FAIL threshold")
	is := rep.Issues[0]
	assert.Equal(t, domain.IssueTypeFaceIdentityDrift, is.Type)
	assert.Equal(t, domain.SeverityHigh, is.Severity)
	assert.Equal(t, "char-a", is.CharacterID)
	assert.Equal(t, "1.0-1.8s", is.TimeRange, "frames 4-6 at 0.25 s: from 1.0 s to the end of frame 6")
	assert.Equal(t, []int{30, 53}, is.Frames)
}

func TestFaceMeasure_ARunBelowOnlyTheWarningThresholdIsMedium(t *testing.T) {
	clip := clipOf(t, frames(t, 0.95, 0.95, 0.7, 0.7, 0.95, 0.95)...)
	rep := measure(t, []judge.FaceSubject{subject(t, "a", 1.0, 1.0)}, clip)

	require.Len(t, rep.Issues, 1)
	assert.Equal(t, domain.SeverityMedium, rep.Issues[0].Severity)
}

func TestFaceMeasure_ARunNeedsTwoFramesAndAMissingFrameEndsIt(t *testing.T) {
	// One bad frame, then two bad frames separated by a missing one: no run reaches two frames.
	clip := clipOf(t, frames(t, 0.95, 0.2, 0.95, 0.95, 0.2, -1, 0.2, 0.95, 0.95, 0.95)...)
	rep := measure(t, []judge.FaceSubject{subject(t, "a", 1.0, 1.0)}, clip)

	assert.Empty(t, rep.Issues)
	assert.True(t, rep.Metrics[0].Evaluated)
}

func TestFaceMeasure_TooManyMissingFramesLeaveTheTakeUnevaluated(t *testing.T) {
	// Only 4 of 10 frames show the face (undetected ratio 0.6 > the 0.5 limit): the low frames
	// among them are not held against the Take and no Issue is raised.
	clip := clipOf(t, frames(t, 0.95, 0.2, 0.2, 0.95, -1, -1, -1, -1, -1, -1)...)
	rep := measure(t, []judge.FaceSubject{subject(t, "a", 1.0, 1.0)}, clip)

	require.Len(t, rep.Metrics, 1)
	assert.False(t, rep.Metrics[0].Evaluated)
	assert.InDelta(t, 0.6, rep.Metrics[0].UndetectedRatio, 1e-6)
	assert.Empty(t, rep.Issues)
}

func TestFaceMeasure_FewDetectedFramesLeaveTheTakeUnevaluated(t *testing.T) {
	clip := clipOf(t, frames(t, 0.95, 0.95)...) // MinDetectedFrames is 3
	rep := measure(t, []judge.FaceSubject{subject(t, "a", 1.0, 1.0)}, clip)

	assert.False(t, rep.Metrics[0].Evaluated)
}

func TestFaceMeasure_UncalibratedReferencesAreNotJudgedByAFixedThreshold(t *testing.T) {
	// Frames at 0.2 would be a FAIL under the old source constants (0.363). They must not be.
	clip := clipOf(t, frames(t, 0.95, 0.2, 0.2, 0.2)...)
	one := judge.NewFaceMeasurer(&fakeEngine{}, judge.DefaultConfig().Face)

	rep, err := one.Measure(context.Background(), []judge.FaceSubject{subject(t, "a", 1.0)}, clip)
	require.NoError(t, err)
	m := rep.Metrics[0]
	assert.False(t, m.Calibrated, "one reference image has no pairwise similarity")
	assert.False(t, m.Evaluated, "without a distribution the numbers do not judge the Take")
	assert.Zero(t, m.WarnThreshold)
	assert.Zero(t, m.FailThreshold)
	assert.InDelta(t, 0.2, m.SimilarityMin, 0.02, "the similarity is still recorded")
	assert.Empty(t, rep.Issues)

	// References that do not resemble each other are not a trustworthy basis either.
	rep, err = one.Measure(context.Background(), []judge.FaceSubject{subject(t, "a", 1.0, 0.2)}, clip)
	require.NoError(t, err)
	assert.False(t, rep.Metrics[0].Calibrated)
	assert.False(t, rep.Metrics[0].Evaluated)
	assert.Zero(t, rep.Metrics[0].WarnThreshold)
	assert.Zero(t, rep.Metrics[0].FailThreshold)
	assert.Empty(t, rep.Issues)
}

func TestFaceMeasure_EachFaceGoesToTheCharacterItResemblesMost(t *testing.T) {
	// Character A is the reference face (red 0), B is a different face (red 255, a quarter turn
	// away). Every frame shows both; the left/right order of the faces is not the order of the subjects.
	engine := &fakeEngine{faces: func(img image.Image) []facesim.Face {
		r, _, _, _ := img.At(0, 0).RGBA()
		if r>>8 == 255 || r>>8 == 0 { // a reference picture: of B (red 255) or of A (red 0)
			return []facesim.Face{{Embedding: embeddingOf(uint8(r >> 8))}}
		}
		// a frame: the face of B first, then the face of A (drifted by red r)
		return []facesim.Face{{Embedding: embeddingOf(255)}, {Embedding: embeddingOf(uint8(r >> 8))}}
	}}
	subjects := []judge.FaceSubject{
		{ID: "b", ReferenceImages: [][]byte{picture(t, 255, 0)}},
		{ID: "a", ReferenceImages: [][]byte{facePicture(t, 1.0)}},
	}
	clip := clipOf(t, frames(t, 0.95, 0.95, 0.95, 0.95)...)

	rep, err := judge.NewFaceMeasurer(engine, judge.DefaultConfig().Face).Measure(context.Background(), subjects, clip)
	require.NoError(t, err)
	byID := map[string]domain.FaceIdentityMetric{}
	for _, m := range rep.Metrics {
		byID[m.CharacterID] = m
	}
	assert.InDelta(t, 1.0, byID["b"].SimilarityMean, 0.01, "B got its own face, not A's")
	assert.InDelta(t, 0.95, byID["a"].SimilarityMean, 0.02)
	assert.Zero(t, byID["a"].UndetectedRatio)
	assert.Empty(t, rep.Issues)
}

func TestFaceMeasure_ACharacterWithoutAFaceInTheFrameIsMissingThere(t *testing.T) {
	// Two characters, one face per frame: the face goes to the better match, the other is missing.
	subjects := []judge.FaceSubject{
		{ID: "b", ReferenceImages: [][]byte{picture(t, 255, 0)}},
		{ID: "a", ReferenceImages: [][]byte{facePicture(t, 1.0)}},
	}
	clip := clipOf(t, frames(t, 0.95, 0.95, 0.95, 0.95)...)

	rep, err := judge.NewFaceMeasurer(&fakeEngine{}, judge.DefaultConfig().Face).Measure(context.Background(), subjects, clip)
	require.NoError(t, err)
	byID := map[string]domain.FaceIdentityMetric{}
	for _, m := range rep.Metrics {
		byID[m.CharacterID] = m
	}
	assert.Zero(t, byID["a"].UndetectedRatio)
	assert.InDelta(t, 1.0, byID["b"].UndetectedRatio, 1e-9, "B never appears")
	assert.False(t, byID["b"].Evaluated)
}

func TestFaceMeasure_ASubjectWhoseReferencesShowNoFaceIsLeftOut(t *testing.T) {
	subjects := []judge.FaceSubject{
		{ID: "ghost", ReferenceImages: [][]byte{noFacePicture(t), []byte("not an image")}},
	}
	rep := measure(t, subjects, clipOf(t, frames(t, 0.9, 0.9, 0.9)...))

	assert.Empty(t, rep.Metrics, "nothing to compare with: the measurement does not apply")
	assert.Empty(t, rep.Issues)
}

func TestFaceMeasure_EngineFailureIsAnError(t *testing.T) {
	boom := errors.New("inference failed")
	m := judge.NewFaceMeasurer(&fakeEngine{err: boom}, judge.DefaultConfig().Face)
	_, err := m.Measure(context.Background(), []judge.FaceSubject{subject(t, "a", 1.0)}, clipOf(t, frames(t, 0.9)...))
	assert.ErrorIs(t, err, boom)
}
