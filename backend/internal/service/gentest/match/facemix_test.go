package match_test

import (
	"context"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/match"
)

// faces finds the same face in every picture (nil: no face anywhere).
type faces struct{ found bool }

func (f faces) Faces(context.Context, image.Image) ([]facesim.Face, error) {
	if !f.found {
		return nil, nil
	}
	return []facesim.Face{{Embedding: facesim.Embedding{1, 0}}}, nil
}

func faceRig(t *testing.T, scores []int, e facesim.Engine, weight float64) rig {
	t.Helper()
	r := gentesttest.New(t)
	f := &gentesttest.FakeMatchLLM{Scores: scores, Err: map[string]error{}}
	m := imagematch.New(f, gentesttest.Target, gentesttest.Target).UseFaces(e, imagematch.FaceMix{Weight: weight, Floor: 0.2, Ceil: 0.7})
	return rig{Rig: r, LLM: f, M: r.UseMatcher(m)}
}

func TestTheFaceMixedScoreDecidesWhenTheMatchStopsAndTheRoundKeepsTheBreakdown(t *testing.T) {
	// the same face is 100 points. The LLM says 70 then 80; at a weight of 0.5 that is 85 then 90: the first round already reaches 85
	r := faceRig(t, []int{70, 80}, faces{found: true}, 0.5)
	v := r.finished(r.start(r.request(match.ModeAuto)).BatchID)
	assert.Equal(t, match.StopReached, v.Stop)
	assert.Equal(t, []int{85}, scores(v), "the score that is compared with the target is the mix")
	sc := v.Rounds[0].Scoring
	require.NotNil(t, sc)
	assert.Equal(t, 70, sc.LLM)
	require.NotNil(t, sc.Face)
	assert.Equal(t, imagematch.FacePart{Cosine: 1, Points: 100, Weight: 0.5}, *sc.Face)

	// and it is in the round the history keeps, so a screen that is opened later shows it too
	again, err := r.M.View(context.Background(), v.BatchID)
	require.NoError(t, err)
	assert.Equal(t, sc, again.Rounds[0].Scoring)
}

func TestWithoutAFaceTheMatchKeepsTheLLMScoreAndSaysWhy(t *testing.T) {
	r := faceRig(t, []int{90}, faces{found: false}, 0.5)
	v := r.finished(r.start(r.request(match.ModeOnce)).BatchID)
	assert.Equal(t, []int{90}, scores(v), "a reference without a face is not pulled down by the face")
	require.NotNil(t, v.Rounds[0].Scoring)
	assert.Nil(t, v.Rounds[0].Scoring.Face)
	assert.Equal(t, imagematch.SkipReferenceNoFace, v.Rounds[0].Scoring.Skipped)
}

func TestWithTheFaceMixOffTheRoundHasNoBreakdown(t *testing.T) {
	r := newRig(t, []int{90})
	v := r.finished(r.start(r.request(match.ModeOnce)).BatchID)
	assert.Nil(t, v.Rounds[0].Scoring)
}
