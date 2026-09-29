package facesim

import (
	"math"
	"math/cmplx"
)

// sfaceSize is the side of the aligned face SFace takes.
const sfaceSize = 112

// sfaceTemplate is where the five landmarks (right eye, left eye, nose tip, right and left mouth
// corner) lie in the aligned 112×112 face: the ArcFace template used by OpenCV's FaceRecognizerSF.
var sfaceTemplate = [5][2]float64{
	{38.2946, 51.6963}, {73.5318, 51.5014}, {56.0252, 71.7366}, {41.5493, 92.3655}, {70.7299, 92.2041},
}

// similarity maps a source point p to a*p + b in the complex plane: a rotation, a uniform scale and
// a translation.
type similarity struct{ a, b complex128 }

// fitSimilarity is the least-squares similarity transform taking src onto dst.
func fitSimilarity(src, dst [5][2]float64) similarity {
	var sx, dx complex128
	for i := range src {
		sx += complex(src[i][0], src[i][1])
		dx += complex(dst[i][0], dst[i][1])
	}
	sx /= complex(float64(len(src)), 0)
	dx /= complex(float64(len(dst)), 0)
	var num complex128
	var den float64
	for i := range src {
		s := complex(src[i][0], src[i][1]) - sx
		d := complex(dst[i][0], dst[i][1]) - dx
		num += cmplx.Conj(s) * d
		den += real(s)*real(s) + imag(s)*imag(s)
	}
	if den == 0 {
		return similarity{a: 1, b: dx - sx}
	}
	a := num / complex(den, 0)
	return similarity{a: a, b: dx - a*sx}
}

// alignFace cuts the 112×112 face of the detection out of m and returns SFace's input tensor
// (NCHW, RGB, 0..255). The detection is in the coordinates of m scaled by 1/scale.
func alignFace(m rgb, d detection, scale float32) []float32 {
	var src [5][2]float64
	for i, l := range d.landmarks {
		src[i] = [2]float64{float64(l[0] / scale), float64(l[1] / scale)}
	}
	t := fitSimilarity(src, sfaceTemplate)
	out := make([]float32, 3*sfaceSize*sfaceSize)
	plane := sfaceSize * sfaceSize
	for y := range sfaceSize {
		for x := range sfaceSize {
			// Inverse mapping: the source point that lands on output pixel (x, y).
			p := (complex(float64(x), float64(y)) - t.b) / t.a
			px, py := real(p), imag(p)
			if math.IsNaN(px) || math.IsNaN(py) {
				continue
			}
			for c := range 3 {
				out[c*plane+y*sfaceSize+x] = m.sample(px, py, c)
			}
		}
	}
	return out
}
