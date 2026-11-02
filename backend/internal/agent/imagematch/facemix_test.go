package imagematch_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/imagematch"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

func TestFaceMixMapsTheCosineToPointsLinearlyAndClampsAtTheEnds(t *testing.T) {
	m := imagematch.FaceMix{Weight: 0.5, Floor: 0.2, Ceil: 0.7}
	for cosine, want := range map[float64]float64{-1: 0, 0.2: 0, 0.45: 50, 0.7: 100, 0.95: 100, 1: 100} {
		assert.InDelta(t, want, m.Points(cosine), 1e-9, "cosine %v", cosine)
	}
}

func TestFaceMixAveragesWithTheWeightAndRoundsToAnInteger(t *testing.T) {
	for name, tc := range map[string]struct {
		weight    float64
		llm, face float64
		want      int
	}{
		"off keeps the LLM":      {0, 80, 0, 80},
		"all face":               {1, 80, 10, 10},
		"half":                   {0.5, 80, 40, 60},
		"rounds up at a half":    {0.5, 61, 40, 51},
		"rounds down below half": {0.3, 50, 51, 50},
		"both at the top":        {0.3, 100, 100, 100},
		"both at the bottom":     {0.3, 0, 0, 0},
	} {
		assert.Equal(t, tc.want, imagematch.FaceMix{Weight: tc.weight, Floor: 0, Ceil: 1}.Mix(tc.llm, tc.face), name)
	}
}

func TestFaceMixIsOffByDefaultAndValidatesItsNumbers(t *testing.T) {
	d := imagematch.DefaultFaceMix()
	assert.False(t, d.Enabled(), "the training data of the face model is not confirmed: the operator turns it on")
	assert.NoError(t, d.Validate())
	for name, m := range map[string]imagematch.FaceMix{
		"weight over 1": {Weight: 1.1, Floor: 0.2, Ceil: 0.7}, "negative weight": {Weight: -0.1, Floor: 0.2, Ceil: 0.7},
		"floor at ceil": {Weight: 0.3, Floor: 0.5, Ceil: 0.5}, "floor over ceil": {Weight: 0.3, Floor: 0.8, Ceil: 0.2},
		"ceil over 1": {Weight: 0.3, Floor: 0.2, Ceil: 1.5},
	} {
		assert.Error(t, m.Validate(), name)
	}
	assert.NoError(t, imagematch.FaceMix{Weight: 1, Floor: -1, Ceil: 1}.Validate())
}

// The color of the corner of a picture says what the fake engine finds in it.
var (
	personA = color.RGBA{R: 255, A: 255} // a face, embedding {1, 0}
	personB = color.RGBA{G: 255, A: 255} // a face, embedding {0.6, 0.8} (cosine 0.6 with person A)
	noFace  = color.RGBA{B: 255, A: 255} // no face
	broken  = color.RGBA{R: 1, G: 1, B: 1, A: 255}
)

type fakeFaces struct{ calls int }

func (f *fakeFaces) Faces(_ context.Context, img image.Image) ([]facesim.Face, error) {
	f.calls++
	r, g, b, _ := img.At(0, 0).RGBA() // 16 bits a channel
	switch {
	case r == 0xffff && g == 0 && b == 0:
		return []facesim.Face{{Embedding: facesim.Embedding{1, 0}}}, nil
	case r == 0 && g == 0xffff && b == 0:
		return []facesim.Face{{Embedding: facesim.Embedding{0.6, 0.8}}}, nil
	case r == 0x0101 && g == 0x0101 && b == 0x0101:
		return nil, errors.New("inference failed")
	}
	return nil, nil
}

func picture(t *testing.T, c color.Color) imagematch.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return imagematch.Image{MimeType: "image/png", Data: buf.Bytes()}
}

func compare(t *testing.T, m *imagematch.Matcher, reference, generated color.Color) imagematch.Judgement {
	t.Helper()
	j, err := m.Compare(t.Context(), picture(t, reference), picture(t, generated))
	require.NoError(t, err)
	return j
}

func mixed(weight float64, engine facesim.Engine) *imagematch.Matcher {
	a := &answer{text: `{"score": 60, "differences": ["背景が違う"]}`}
	return imagematch.New(a, target, target).UseFaces(engine, imagematch.FaceMix{Weight: weight, Floor: 0.2, Ceil: 0.7})
}

func TestCompareMixesTheFaceIntoTheScoreAndSaysHow(t *testing.T) {
	// the cosine of person A and person B is 0.6: (0.6-0.2)/(0.7-0.2) = 80 points; 0.7 x 60 + 0.3 x 80 = 66
	j := compare(t, mixed(0.3, &fakeFaces{}), personA, personB)
	assert.Equal(t, 66, j.Score)
	require.NotNil(t, j.Scoring)
	assert.Equal(t, 60, j.Scoring.LLM)
	require.NotNil(t, j.Scoring.Face)
	assert.Equal(t, imagematch.FacePart{Cosine: 0.6, Points: 80, Weight: 0.3}, *j.Scoring.Face)
	assert.Empty(t, j.Scoring.Skipped)
	assert.Equal(t, []string{"背景が違う"}, j.Differences, "what differs is still what the LLM said")

	same := compare(t, mixed(0.3, &fakeFaces{}), personA, personA)
	assert.Equal(t, 0.7*60+0.3*100, float64(same.Score), "the same face is 100 points")
}

func TestCompareFallsBackToTheLLMScoreWhenTheFaceCannotBeUsed(t *testing.T) {
	for name, tc := range map[string]struct {
		engine     facesim.Engine
		ref, gen   color.Color
		wantReason string
	}{
		"the reference has no face (a landscape)": {&fakeFaces{}, noFace, personA, imagematch.SkipReferenceNoFace},
		"the generated image has no face":         {&fakeFaces{}, personA, noFace, imagematch.SkipGeneratedNoFace},
		"the face engine fails":                   {&fakeFaces{}, personA, broken, imagematch.SkipError},
		"the face models are not available":       {nil, personA, personB, imagematch.SkipUnavailable},
	} {
		j := compare(t, mixed(0.3, tc.engine), tc.ref, tc.gen)
		assert.Equal(t, 60, j.Score, name+": the LLM's score alone")
		require.NotNil(t, j.Scoring, name)
		assert.Nil(t, j.Scoring.Face, name)
		assert.Equal(t, 60, j.Scoring.LLM, name)
		assert.Equal(t, tc.wantReason, j.Scoring.Skipped, name)
	}
}

func TestCompareWithoutTheMixIsTheLLMAloneAndDoesNotTouchTheFaceEngine(t *testing.T) {
	engine := &fakeFaces{}
	j := compare(t, mixed(0, engine), personA, personB)
	assert.Equal(t, 60, j.Score)
	assert.Nil(t, j.Scoring, "nothing to explain when the face is not mixed in")
	assert.Zero(t, engine.calls)

	plain := imagematch.New(&answer{text: `{"score": 60}`}, target, target)
	assert.Nil(t, compare(t, plain, personA, personB).Scoring)
}

func TestCompareDoesNotFailWhenAPictureCannotBeDecodedForTheFace(t *testing.T) {
	m := mixed(0.3, &fakeFaces{})
	j, err := m.Compare(t.Context(), imagematch.Image{MimeType: "image/png", Data: []byte("not a picture")}, picture(t, personA))
	require.NoError(t, err, "the LLM answered: the round has a score")
	assert.Equal(t, 60, j.Score)
	require.NotNil(t, j.Scoring)
	assert.Equal(t, imagematch.SkipError, j.Scoring.Skipped)
}
