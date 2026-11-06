package markupcheck_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/markupcheck"
)

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func nrgba(w, h int, a uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{R: 255, A: a})
		}
	}
	return img
}

// jpegWithOrientation is a w × h JPEG with an EXIF APP1 segment that says the orientation (1..8).
func jpegWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil))
	raw := buf.Bytes()
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01") // big endian, IFD at 8, one entry
	entry := make([]byte, 12)
	binary.BigEndian.PutUint16(entry[0:], 0x0112)
	binary.BigEndian.PutUint16(entry[2:], 3)
	binary.BigEndian.PutUint32(entry[4:], 1)
	binary.BigEndian.PutUint16(entry[8:], uint16(orientation))
	body := append(append([]byte("Exif\x00\x00"), tiff...), entry...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(body)+2))
	return append(append(append([]byte{}, raw[:2]...), append(seg, body...)...), raw[2:]...)
}

func TestCheck(t *testing.T) {
	base := pngOf(t, nrgba(6, 4, 255))
	holes := nrgba(6, 4, 255)
	holes.Set(1, 1, color.NRGBA{})

	for name, tc := range map[string]struct {
		markup, base []byte
		want         string // "" is accepted
	}{
		"a transparent layer of the same size":       {pngOf(t, nrgba(6, 4, 0)), base, ""},
		"a layer with a few transparent pixels":      {pngOf(t, holes), base, ""},
		"a different size":                           {pngOf(t, nrgba(3, 4, 0)), base, "6 × 4"},
		"nothing transparent":                        {pngOf(t, nrgba(6, 4, 255)), base, "透明な部分がありません"},
		"an opaque picture without an alpha channel": {pngOf(t, image.NewGray(image.Rect(0, 0, 6, 4))), base, "透明な部分がありません"},
		"not an image":                               {[]byte("nope"), base, "PNG"},
		"a JPEG is not a PNG":                        {jpegWithOrientation(t, 6, 4, 1), base, "PNG"},
		"the base is unreadable":                     {pngOf(t, nrgba(6, 4, 0)), []byte("nope"), "元の画像"},
		"a base turned by its orientation":           {pngOf(t, nrgba(4, 6, 0)), jpegWithOrientation(t, 6, 4, 6), ""},
		"a base turned, the markup not":              {pngOf(t, nrgba(6, 4, 0)), jpegWithOrientation(t, 6, 4, 6), "4 × 6"},
		"a base mirrored keeps its size":             {pngOf(t, nrgba(6, 4, 0)), jpegWithOrientation(t, 6, 4, 2), ""},
	} {
		t.Run(name, func(t *testing.T) {
			err := markupcheck.Check(tc.markup, tc.base)
			if tc.want == "" {
				assert.NoError(t, err)
				return
			}
			if assert.Error(t, err) {
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

// A small file of a huge size (a PNG packs a flat picture very small) is refused from its header, before it is decoded.
func TestCheckRefusesAMarkupOverTheLimitWithoutDecodingIt(t *testing.T) {
	const side = 4097 // 4097 × 4097 = 16,785,409 > 16,777,216
	err := markupcheck.Check(pngOf(t, nrgba(side, side, 0)), pngOf(t, nrgba(side, side, 255)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "大きすぎます")
}

func TestCheckRefusesAMarkupOverFiftyMegabytes(t *testing.T) {
	err := markupcheck.Check(make([]byte, markupcheck.MaxBytes+1), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "50 MB")
}
