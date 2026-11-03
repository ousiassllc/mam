package imgprep_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/imgprep"
)

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

// disc is a dark disc of radius r on a mid-gray picture: a picture with one clear contour and flat areas on both sides of it.
func disc(size, r int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := size / 2
	for y := range size {
		for x := range size {
			v := uint8(170)
			if (x-c)*(x-c)+(y-c)*(y-c) <= r*r {
				v = 40
			}
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

func decodeGray(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return img
}

func white(img image.Image, x, y int) bool { r, _, _, _ := img.At(x, y).RGBA(); return r > 0x8000 }

// #993: a photograph becomes white lines on black along its contours and nothing else.
func TestCannyDrawsTheContourOfAPictureAndNothingElse(t *testing.T) {
	out, made, err := imgprep.Canny(encode(t, disc(128, 40)))
	require.NoError(t, err)
	assert.True(t, made)
	edges := decodeGray(t, out)
	assert.Equal(t, image.Rect(0, 0, 128, 128), edges.Bounds())

	var onRing, elsewhere int
	for y := range 128 {
		for x := range 128 {
			if !white(edges, x, y) {
				continue
			}
			d2 := (x-64)*(x-64) + (y-64)*(y-64)
			if d2 >= 37*37 && d2 <= 43*43 {
				onRing++
			} else {
				elsewhere++
			}
		}
	}
	assert.Greater(t, onRing, 2*3*40, "the circle (circumference about 250 px) is drawn")
	assert.Zero(t, elsewhere, "no line inside the disc or in the flat background")
	assert.False(t, white(edges, 64, 64))
	assert.False(t, white(edges, 5, 5))
}

func TestCannyOfAFlatPictureHasNoEdges(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	out, made, err := imgprep.Canny(encode(t, img))
	require.NoError(t, err)
	assert.True(t, made)
	for y := range 64 {
		for x := range 64 {
			require.False(t, white(decodeGray(t, out), x, y))
		}
	}
}

func TestCannyReadsJPEGAndKeepsATransparentPictureOnWhite(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, disc(64, 20), nil))
	_, made, err := imgprep.Canny(b.Bytes())
	require.NoError(t, err)
	assert.True(t, made)

	clear := image.NewNRGBA(image.Rect(0, 0, 32, 32)) // fully transparent: white, flat, no edges
	out, made, err := imgprep.Canny(encode(t, clear))
	require.NoError(t, err)
	assert.True(t, made, "a white picture is not an edge map (white lines on black)")
	assert.False(t, white(decodeGray(t, out), 16, 16))
}

// An edge map the user made is the control image as it is: it must not be edge-detected a second time (that would double every line).
func TestCannyLeavesAnEdgeMapAsItIs(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 64, 64))
	for x := 10; x < 54; x++ {
		img.SetGray(x, 20, color.Gray{255})
		img.SetGray(x, 44, color.Gray{255})
	}
	src := encode(t, img)
	out, made, err := imgprep.Canny(src)
	require.NoError(t, err)
	assert.False(t, made)
	assert.Equal(t, src, out)
}

func TestCannyScalesALargePictureDown(t *testing.T) {
	out, made, err := imgprep.Canny(encode(t, disc(2048, 600)))
	require.NoError(t, err)
	assert.True(t, made)
	assert.Equal(t, image.Rect(0, 0, imgprep.MaxSide, imgprep.MaxSide), decodeGray(t, out).Bounds())
}

func TestCannyRefusesWhatIsNotAPicture(t *testing.T) {
	_, _, err := imgprep.Canny([]byte("not a picture"))
	assert.True(t, errors.Is(err, imgprep.ErrNotPicture))
	_, _, err = imgprep.Canny(nil)
	assert.True(t, errors.Is(err, imgprep.ErrNotPicture))
}

func TestApplyKeepsTheTypeOfAnUnchangedPictureAndRefusesUnknownKinds(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	img.SetGray(8, 8, color.Gray{255})
	src := encode(t, img)
	out, mime, err := imgprep.Apply(imgprep.KindCanny, src, "image/png")
	require.NoError(t, err)
	assert.Equal(t, src, out)
	assert.Equal(t, "image/png", mime)

	_, mime, err = imgprep.Apply(imgprep.KindCanny, encode(t, disc(64, 20)), "image/jpeg")
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime, "the edge map is a PNG whatever the picture was")

	_, _, err = imgprep.Apply("openpose", src, "image/png")
	assert.Error(t, err)
}
