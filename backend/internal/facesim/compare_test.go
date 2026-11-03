package facesim

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	return buf.Bytes()
}

func TestCompareIsTheSimilarityOfTheLargestFacesOfTwoFiles(t *testing.T) {
	file := onePixelPNG(t)
	eng := &scripted{answers: []scriptedAnswer{{faces: face(1, 0)}, {faces: face(0.6, 0.8)}}}

	got, err := Compare(t.Context(), eng, file, file)

	require.NoError(t, err)
	assert.Equal(t, BothHaveFaces, got.Missing)
	assert.InDelta(t, 0.6, got.Cosine, 1e-6)
	assert.Greater(t, got.Cosine, SameFaceThreshold, "0.6 is above the threshold of SFace")
}

func TestCompareNamesThePictureWithoutAFace(t *testing.T) {
	file := onePixelPNG(t)

	got, err := Compare(t.Context(), &scripted{answers: []scriptedAnswer{{faces: face(1, 0)}, {}}}, file, file)

	require.NoError(t, err)
	assert.Equal(t, SecondHasNoFace, got.Missing)
}

func TestCompareRefusesAFileThatIsNotAPicture(t *testing.T) {
	file := onePixelPNG(t)
	eng := &scripted{}

	_, err := Compare(t.Context(), eng, []byte("not a picture"), file)
	require.ErrorContains(t, err, "decode the reference")
	_, err = Compare(t.Context(), eng, file, []byte("not a picture"))
	require.ErrorContains(t, err, "decode the generated image")
	assert.Zero(t, eng.calls, "no face is looked for in a file that cannot be read")
}
