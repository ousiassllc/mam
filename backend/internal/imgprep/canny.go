// Package imgprep makes the control image of a ControlNet workflow inside mam (Issue #993): a Canny edge map, in pure Go.
//
// controlnet_aux (the ComfyUI custom node that does this) needs OpenCV, scikit-image, onnxruntime and more, none of which the bundled
// Python environment has, and mam runs no pip (spec/architecture/model-manager/custom-nodes.md §5.1). Canny needs no weights and no
// model, so there is no licence to check either; the preprocessors that do (OpenPose, DWPose, depth) are not done here.
package imgprep

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // the types of a reference image of the generation test
	"image/png"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // ... and WebP
)

// The values of the edge detection. The thresholds are the ones of controlnet_aux's CannyEdgePreprocessor (100 and 200 on OpenCV's
// 3x3 Sobel, which is not normalised); a light blur keeps the noise of a photograph from becoming edges.
const (
	lowThreshold  = 100
	highThreshold = 200
	blurSigma     = 1.0
	// MaxSide is the longest side of the edge map: a larger picture is scaled down first (the ControlNet scales the control image to the
	// size of the generation anyway), which keeps the cost and the thickness of the lines bounded.
	MaxSide = 1024
	// MaxPixels is the largest picture that is read at all (a decode bomb guard).
	MaxPixels = 64 << 20
)

// ErrNotPicture means the bytes are not a PNG, JPEG or WebP picture of a size that is read.
var ErrNotPicture = errors.New("imgprep: not a readable picture")

// Canny returns the control image for a canny ControlNet made from the picture in data (PNG, JPEG or WebP): white edges on black, as an
// 8-bit gray PNG, at most MaxSide pixels on the long side. A picture that already is an edge map (nearly black and white, mostly black:
// what a user who prepared the control image gives) is returned as it is, so that giving mam an edge map and giving it a photograph
// both work. The second result tells whether the edge map was made (false: data was returned unchanged).
func Canny(data []byte) (out []byte, made bool, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, false, errors.Join(ErrNotPicture, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels {
		return nil, false, fmt.Errorf("%w: %d x %d pixels", ErrNotPicture, cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false, errors.Join(ErrNotPicture, err)
	}
	g := grayOnWhite(src)
	if isEdgeMap(g) {
		return data, false, nil
	}
	g = fit(g, MaxSide)
	edges := detect(g, lowThreshold, highThreshold)
	var buf bytes.Buffer
	if err := png.Encode(&buf, edges); err != nil {
		return nil, false, fmt.Errorf("imgprep: encode the edge map: %w", err)
	}
	return buf.Bytes(), true, nil
}

// grayOnWhite is the luminance of src, a transparent picture counting as on white (like the previews of the picture elsewhere in mam).
func grayOnWhite(src image.Image) *image.Gray {
	b := src.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			c := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			a := float64(c.A) / 255
			l := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
			g.Pix[y*g.Stride+x] = uint8(math.Round(l*a + 255*(1-a)))
		}
	}
	return g
}

// isEdgeMap tells a picture that is an edge map already: at least 97% of its pixels are nearly black or nearly white, and not more than
// half are white (white lines on black; black lines on white are a drawing, not what a canny ControlNet takes, and go through the edge
// detection like a photograph).
func isEdgeMap(g *image.Gray) bool {
	var dark, light int
	for y := range g.Rect.Dy() {
		for _, v := range g.Pix[y*g.Stride : y*g.Stride+g.Rect.Dx()] {
			switch {
			case v <= 48:
				dark++
			case v >= 207:
				light++
			}
		}
	}
	n := g.Rect.Dx() * g.Rect.Dy()
	return float64(dark+light) >= 0.97*float64(n) && light*2 <= n
}

// fit scales g down so that its long side is at most side; a smaller picture is kept.
func fit(g *image.Gray, side int) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	long := max(w, h)
	if long <= side {
		return g
	}
	w, h = max(1, w*side/long), max(1, h*side/long)
	dst := image.NewGray(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), g, g.Bounds(), draw.Src, nil)
	return dst
}

// detect is the Canny edge detector: a Gaussian blur, the Sobel gradient (|gx|+|gy|, like OpenCV's default), non-maximum suppression along
// the gradient and hysteresis with the two thresholds. The edges are 255 on 0.
func detect(g *image.Gray, low, high float32) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	out := image.NewGray(image.Rect(0, 0, w, h))
	if w < 3 || h < 3 {
		return out
	}
	pix := make([]float32, w*h)
	for y := range h {
		for x := range w {
			pix[y*w+x] = float32(g.Pix[y*g.Stride+x])
		}
	}
	pix = blur(pix, w, h, blurSigma)
	mag, dir := sobel(pix, w, h)
	thin := suppress(mag, dir, w, h)
	for i, on := range hysteresis(thin, w, h, low, high) {
		if on {
			out.Pix[(i/w)*out.Stride+i%w] = 255
		}
	}
	return out
}

// blur is the separable Gaussian blur of pix; the border repeats its edge pixels.
func blur(pix []float32, w, h int, sigma float64) []float32 {
	r := int(math.Ceil(3 * sigma))
	k := make([]float32, 2*r+1)
	var sum float32
	for i := range k {
		d := float64(i - r)
		k[i] = float32(math.Exp(-d * d / (2 * sigma * sigma)))
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	tmp := make([]float32, len(pix))
	for y := range h {
		for x := range w {
			var s float32
			for i, kv := range k {
				s += kv * pix[y*w+min(max(x+i-r, 0), w-1)]
			}
			tmp[y*w+x] = s
		}
	}
	out := make([]float32, len(pix))
	for y := range h {
		for x := range w {
			var s float32
			for i, kv := range k {
				s += kv * tmp[min(max(y+i-r, 0), h-1)*w+x]
			}
			out[y*w+x] = s
		}
	}
	return out
}

// sobel is the gradient magnitude |gx|+|gy| of pix (3x3 Sobel, not normalised; the border is 0) and the direction of the gradient of each
// pixel as one of four sectors: 0 horizontal (the edge is vertical), 1 towards down-right, 2 vertical, 3 towards down-left (y is down).
func sobel(pix []float32, w, h int) (mag []float32, dir []uint8) {
	mag = make([]float32, w*h)
	dir = make([]uint8, w*h)
	const tan22, tan67 = 0.41421357, 2.4142135 // tan(22.5°), tan(67.5°)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			a, b, c := pix[i-w-1], pix[i-w], pix[i-w+1]
			d, f := pix[i-1], pix[i+1]
			g0, g1, g2 := pix[i+w-1], pix[i+w], pix[i+w+1]
			gx := (c + 2*f + g2) - (a + 2*d + g0)
			gy := (g0 + 2*g1 + g2) - (a + 2*b + c)
			ax, ay := float32(math.Abs(float64(gx))), float32(math.Abs(float64(gy)))
			mag[i] = ax + ay
			switch {
			case ay <= ax*tan22:
				dir[i] = 0
			case ay >= ax*tan67:
				dir[i] = 2
			case (gx > 0) == (gy > 0):
				dir[i] = 1
			default:
				dir[i] = 3
			}
		}
	}
	return mag, dir
}

// suppress keeps the pixels whose magnitude is a local maximum along the gradient (the ridge of an edge): the thick band of a blurred edge
// becomes a line one pixel wide. A tie with the neighbour on one side is kept once.
func suppress(mag []float32, dir []uint8, w, h int) []float32 {
	out := make([]float32, len(mag))
	offsets := [4][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}} // (dx, dy) along the gradient of each sector, y down
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			m := mag[i]
			if m == 0 {
				continue
			}
			o := offsets[dir[i]]
			ahead := mag[(y+o[1])*w+x+o[0]]
			behind := mag[(y-o[1])*w+x-o[0]]
			if m > ahead && m >= behind {
				out[i] = m
			}
		}
	}
	return out
}

// hysteresis marks the pixels of mag that are at least high, and the pixels of at least low that touch one of them (eight neighbours,
// through other such pixels).
func hysteresis(mag []float32, w, h int, low, high float32) []bool {
	on := make([]bool, len(mag))
	var stack []int
	for i, m := range mag {
		if m >= high {
			on[i] = true
			stack = append(stack, i)
		}
	}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := i%w, i/w
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				nx, ny := x+dx, y+dy
				if nx < 0 || ny < 0 || nx >= w || ny >= h {
					continue
				}
				j := ny*w + nx
				if !on[j] && mag[j] >= low {
					on[j] = true
					stack = append(stack, j)
				}
			}
		}
	}
	return on
}
