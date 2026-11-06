// Package markupcheck judges the markup image of a GPT Image 2.6 edit (Issue #1096). The Image API lays it over the first input image, and takes it only
// when (API reference, Create image edit: `markup`) it is a PNG with transparent pixels, has the same pixel size as that image (after its orientation), is
// at most 16,777,216 pixels and at most 50 MB. mam refuses a markup that breaks one of these before the call, with the reason, instead of sending it to
// be refused (and paid for in time) by OpenAI.
package markupcheck

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/jpeg" // the types of the first input image
	_ "image/png"  // the markup itself, and an input image

	_ "golang.org/x/image/webp"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// The limits of the markup the Image API states.
const (
	MaxPixels = 16_777_216
	MaxBytes  = 50 << 20
)

// Check judges markup against base, the first input image it is laid over (both are the files as they are stored).
func Check(markup, base []byte) error {
	if len(markup) > MaxBytes {
		return gterr.Invalid("マークアップ画像は %d MB 以下にしてください", MaxBytes>>20)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(markup))
	if err != nil || format != "png" {
		return gterr.Invalid("マークアップ画像は透明部分のある PNG にしてください")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxPixels/cfg.Height {
		return gterr.Invalid("マークアップ画像が大きすぎます（%d × %d）。%d 画素以下にしてください", cfg.Width, cfg.Height, MaxPixels)
	}
	bw, bh, err := sizeOf(base)
	if err != nil {
		return gterr.Invalid("マークアップを重ねる元の画像（入力画像の 1 枚目）を読めません")
	}
	if bw != cfg.Width || bh != cfg.Height {
		return gterr.Invalid("マークアップ画像の大きさ（%d × %d）が、重ねる元の画像（入力画像の 1 枚目。向きを直した後）の大きさ（%d × %d）と違います。同じ大きさにしてください", cfg.Width, cfg.Height, bw, bh)
	}
	img, _, err := image.Decode(bytes.NewReader(markup))
	if err != nil {
		return gterr.Invalid("マークアップ画像は透明部分のある PNG にしてください")
	}
	if !hasTransparency(img) {
		return gterr.Invalid("マークアップ画像に透明な部分がありません。元の画像に重ねる印だけを描き、ほかは透明にしてください")
	}
	return nil
}

// hasTransparency reports whether a pixel of img is not fully opaque. Every image type of the standard library that has an alpha channel (and Gray and
// YCbCr, which have none) answers Opaque from its pixels or its palette; any other type is read pixel by pixel.
func hasTransparency(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return !o.Opaque()
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xFFFF {
				return true
			}
		}
	}
	return false
}

// sizeOf is the pixel size of an image as it is shown: a JPEG whose EXIF orientation turns it by a quarter (5 to 8) has its sides swapped, which is what the Image
// API compares the markup with. (A PNG or WebP carries its orientation elsewhere or not at all; its stored size is used.)
func sizeOf(data []byte) (w, h int, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	if format == "jpeg" && orientation(data) >= 5 {
		return cfg.Height, cfg.Width, nil
	}
	return cfg.Width, cfg.Height, nil
}

// orientation reads the EXIF orientation (1 to 8) of a JPEG, 1 when there is none.
func orientation(jpeg []byte) int {
	i := 2 // after the SOI marker
	for i+4 <= len(jpeg) && jpeg[i] == 0xFF {
		marker, n := jpeg[i+1], int(binary.BigEndian.Uint16(jpeg[i+2:i+4]))
		if marker == 0xDA || n < 2 || i+2+n > len(jpeg) { // the scan starts, or the segment is broken
			return 1
		}
		if seg := jpeg[i+4 : i+2+n]; marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

// tiffOrientation reads tag 0x0112 of the first IFD of an EXIF TIFF block.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	n := int(order.Uint16(t[ifd : ifd+2]))
	for k := range n {
		e := ifd + 2 + 12*k
		if e+12 > len(t) {
			break
		}
		if order.Uint16(t[e:e+2]) == 0x0112 {
			if v := int(order.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
