package imagematch

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // the types of an input image of the generation test
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // ... and WebP
)

// Limits of a picture shown to the LLM: the vendors refuse large files (5 MB for Anthropic), and a picture of more than 1024 pixels
// on its long side does not make the comparison better.
const (
	maxSide      = 1024
	maxKeepBytes = 3 << 20
	jpegQuality  = 88
)

// Prepare makes a picture ready for the LLM: one that is small enough is sent as it is, a larger one is scaled down to 1024 pixels
// on its long side and sent as JPEG. The type is detected from the bytes.
func Prepare(data []byte) (Image, error) {
	mime := http.DetectContentType(data)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("imagematch: read the picture: %w", err)
	}
	if max(cfg.Width, cfg.Height) <= maxSide && len(data) <= maxKeepBytes && mime != "image/webp" {
		return Image{MimeType: mime, Data: data}, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, fmt.Errorf("imagematch: decode the picture: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > maxSide {
		w, h = max(1, w*maxSide/long), max(1, h*maxSide/long)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src) // a transparent picture becomes JPEG on white
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return Image{}, fmt.Errorf("imagematch: encode the picture: %w", err)
	}
	return Image{MimeType: "image/jpeg", Data: out.Bytes()}, nil
}
