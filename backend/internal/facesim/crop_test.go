package facesim

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
)

// fakeFaces is an Engine that finds the faces it is given, whatever the picture is.
type fakeFaces struct {
	faces []Face
	err   error
	seen  image.Rectangle
}

func (f *fakeFaces) Faces(_ context.Context, img image.Image) ([]Face, error) {
	f.seen = img.Bounds()
	return f.faces, f.err
}

// pngOf is a w×h picture that is red inside the rectangle red and blue elsewhere, so that a crop can be told by its pixels.
func pngOf(t *testing.T, w, h int, red image.Rectangle) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{B: 255, A: 255}
			if image.Pt(x, y).In(red) {
				c = color.NRGBA{R: 255, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return img
}

// The crop is a square around the largest face, with the surroundings of it and nothing of the far corners of the picture.
func TestCropFace_CutsASquareAroundTheLargestFace(t *testing.T) {
	face := image.Rect(400, 300, 500, 420) // 100×120
	src := pngOf(t, 1000, 1000, face)
	eng := &fakeFaces{faces: []Face{{Box: face, Score: 0.9}, {Box: image.Rect(10, 10, 30, 30), Score: 0.9}}}

	got, ok, err := CropFace(t.Context(), eng, src)

	require.NoError(t, err)
	require.True(t, ok)
	b := decode(t, got).Bounds()
	assert.Equal(t, 240, b.Dx(), "twice the larger side of the face")
	assert.Equal(t, 240, b.Dy(), "square")
	// the face is in the crop (its red is there), and is not the whole of it
	img := decode(t, got)
	red := 0
	for y := range b.Dy() {
		for x := range b.Dx() {
			if r, _, _, _ := img.At(x, y).RGBA(); r > 0 {
				red++
			}
		}
	}
	assert.Equal(t, 100*120, red, "the whole face is in the crop")
}

// A face at the edge is not squeezed out of a smaller crop: the square moves into the picture.
func TestCropRect_StaysInsideThePicture(t *testing.T) {
	bounds := image.Rect(0, 0, 1000, 800)
	for _, box := range []image.Rectangle{image.Rect(0, 0, 100, 100), image.Rect(900, 700, 1000, 800), image.Rect(450, 0, 550, 100)} {
		r, ok := cropRect(box, bounds)
		require.True(t, ok, box)
		assert.True(t, r.In(bounds), "%v is not inside %v", r, bounds)
		assert.Equal(t, 200, r.Dx(), box)
		assert.True(t, box.In(r), "the face is in the crop: %v in %v", box, r)
	}
}

// What is not worth cutting is left alone: no face, a face that fills the picture, a face too small to give more.
func TestCropFace_LeavesThePictureWhenCuttingGainsNothing(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		eng  *fakeFaces
	}{
		{"no face", 800, 800, &fakeFaces{}},
		{"a close-up", 512, 512, &fakeFaces{faces: []Face{{Box: image.Rect(100, 100, 400, 420)}}}},
		{"a face of 20 pixels", 2000, 2000, &fakeFaces{faces: []Face{{Box: image.Rect(900, 900, 920, 920)}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := CropFace(t.Context(), tt.eng, pngOf(t, tt.w, tt.h, image.Rectangle{}))
			require.NoError(t, err)
			assert.False(t, ok)
			assert.Nil(t, got)
		})
	}
}

func TestCropFace_Errors(t *testing.T) {
	boom := errors.New("the model failed")
	_, ok, err := CropFace(t.Context(), &fakeFaces{err: boom}, pngOf(t, 100, 100, image.Rectangle{}))
	require.ErrorIs(t, err, boom)
	assert.False(t, ok)

	_, ok, err = CropFace(t.Context(), &fakeFaces{}, []byte("not an image"))
	require.Error(t, err)
	assert.False(t, ok)
}
