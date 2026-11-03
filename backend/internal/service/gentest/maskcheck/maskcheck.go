// Package maskcheck judges the mask of a partial redraw (Issue #974): its size is read from the header before it is decoded, and it must have a white
// part (the part that is redrawn).
package maskcheck

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// MaxMaskPixels is the most pixels (width × height) a mask has: 8192 × 8192, as many as the largest picture of "make it larger". The size is
// read from the header before the mask is decoded, so that a small file of a huge size (a PNG packs a flat picture very small) is refused
// instead of filling the memory.
const MaxMaskPixels = 64 << 20

// whiteLevel is the 8-bit level from which a channel counts as white (the same as 0xC000 of 16 bits).
const whiteLevel = 0xC0

// Paints reports whether the mask has a near-white pixel (the part that is redrawn); it stops at the first one.
func Paints(data []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return gterr.Invalid("マスクは PNG・JPEG・WebP の画像にしてください")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxMaskPixels/cfg.Height {
		return gterr.Invalid("マスク画像が大きすぎます（%d × %d）。%d 万画素以下にしてください", cfg.Width, cfg.Height, MaxMaskPixels/10000)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return gterr.Invalid("マスクは PNG・JPEG・WebP の画像にしてください")
	}
	if hasWhitePixel(img) {
		return nil
	}
	return gterr.Invalid("マスクが真っ黒です。描き直す部分を白く塗ってください")
}

// hasWhitePixel looks for a near-white pixel; the common types are read from their pixel bytes, any other through At.
func hasWhitePixel(img image.Image) bool {
	b := img.Bounds()
	switch m := img.(type) {
	case *image.Gray:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for _, v := range m.Pix[m.PixOffset(b.Min.X, y) : m.PixOffset(b.Max.X-1, y)+1] {
				if v >= whiteLevel {
					return true
				}
			}
		}
		return false
	case *image.RGBA:
		return hasWhiteRGBA(m, b, m.Pix, m.PixOffset)
	case *image.NRGBA:
		return hasWhiteRGBA(m, b, m.Pix, m.PixOffset)
	}
	return hasWhiteAt(img, b)
}

// hasWhiteRGBA reads the 4-byte pixels of an RGBA or NRGBA directly; a pixel that is not opaque goes through At.
func hasWhiteRGBA(img image.Image, b image.Rectangle, pix []uint8, offset func(x, y int) int) bool {
	for y := b.Min.Y; y < b.Max.Y; y++ {
		i := offset(b.Min.X, y)
		for x := b.Min.X; x < b.Max.X; x, i = x+1, i+4 {
			if pix[i+3] == 0xFF {
				if pix[i] >= whiteLevel && pix[i+1] >= whiteLevel && pix[i+2] >= whiteLevel {
					return true
				}
			} else if whitePixel(img, x, y) {
				return true
			}
		}
	}
	return false
}

func hasWhiteAt(img image.Image, b image.Rectangle) bool {
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if whitePixel(img, x, y) {
				return true
			}
		}
	}
	return false
}

func whitePixel(img image.Image, x, y int) bool {
	r, g, bl, _ := img.At(x, y).RGBA()
	return r > 0xC000 && g > 0xC000 && bl > 0xC000
}
