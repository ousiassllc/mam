package facesim

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scripted is an Engine that answers the images in the order it is asked.
type scripted struct {
	answers []scriptedAnswer
	calls   int
}

type scriptedAnswer struct {
	faces []Face
	err   error
}

func (s *scripted) Faces(context.Context, image.Image) ([]Face, error) {
	a := s.answers[s.calls]
	s.calls++
	return a.faces, a.err
}

func face(e ...float32) []Face { return []Face{{Embedding: e}} }

func TestSimilarityIsTheCosineOfTheLargestFaces(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	// the first face of each answer is the largest one: the second is ignored
	eng := &scripted{answers: []scriptedAnswer{
		{faces: append(face(1, 0), Face{Embedding: Embedding{0, 1}})}, {faces: append(face(0.6, 0.8), Face{Embedding: Embedding{1, 0}})},
	}}
	sim, missing, err := Similarity(t.Context(), eng, img, img)
	require.NoError(t, err)
	assert.Equal(t, BothHaveFaces, missing)
	assert.InDelta(t, 0.6, sim, 1e-6)
}

func TestSimilarityNamesTheImageWithoutAFaceAndStopsEarly(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	first := &scripted{answers: []scriptedAnswer{{}, {faces: face(1, 0)}}}
	_, missing, err := Similarity(t.Context(), first, img, img)
	require.NoError(t, err)
	assert.Equal(t, FirstHasNoFace, missing)
	assert.Equal(t, 1, first.calls, "a reference without a face needs no look at the other picture")

	second := &scripted{answers: []scriptedAnswer{{faces: face(1, 0)}, {}}}
	_, missing, err = Similarity(t.Context(), second, img, img)
	require.NoError(t, err)
	assert.Equal(t, SecondHasNoFace, missing)
}

func TestSimilarityPassesTheEngineError(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	boom := errors.New("boom")
	for name, answers := range map[string][]scriptedAnswer{
		"first": {{err: boom}}, "second": {{faces: face(1, 0)}, {err: boom}},
	} {
		_, _, err := Similarity(t.Context(), &scripted{answers: answers}, img, img)
		assert.ErrorIs(t, err, boom, name)
	}
}
