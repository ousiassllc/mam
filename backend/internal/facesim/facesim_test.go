package facesim

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCosine(t *testing.T) {
	assert.InDelta(t, 1.0, Cosine(Embedding{1, 2, 3}, Embedding{2, 4, 6}), 1e-9, "scale does not matter")
	assert.InDelta(t, 0.0, Cosine(Embedding{1, 0}, Embedding{0, 1}), 1e-9)
	assert.InDelta(t, -1.0, Cosine(Embedding{1, 1}, Embedding{-1, -1}), 1e-9)
	assert.Zero(t, Cosine(Embedding{1, 2}, Embedding{1, 2, 3}), "different lengths cannot be compared")
	assert.Zero(t, Cosine(nil, nil))
	assert.Zero(t, Cosine(Embedding{0, 0}, Embedding{1, 1}), "a zero vector has no direction")
}

func TestNormalise_ScalesToUnitLengthAndKeepsZero(t *testing.T) {
	v := []float32{3, 4}
	normalise(v)
	assert.InDelta(t, 0.6, v[0], 1e-6)
	assert.InDelta(t, 0.8, v[1], 1e-6)
	z := []float32{0, 0}
	normalise(z)
	assert.Equal(t, []float32{0, 0}, z)
}

// yunetFixture is the raw output of an input of `size` pixels in which every cell is empty.
func yunetFixture(size int) yunetOutputs {
	out := yunetOutputs{cls: map[int][]float32{}, obj: map[int][]float32{}, bbox: map[int][]float32{}, kps: map[int][]float32{}}
	for _, s := range yunetStrides {
		n := (size / s) * (size / s)
		out.cls[s], out.obj[s] = make([]float32, n), make([]float32, n)
		out.bbox[s], out.kps[s] = make([]float32, n*4), make([]float32, n*10)
	}
	return out
}

// put makes cell (row, col) of the stride a face of the score whose box is w×h around the cell centre.
func put(out yunetOutputs, stride, cols, row, col int, score float32, w, h float64) {
	i := row*cols + col
	out.cls[stride][i], out.obj[stride][i] = score, score
	out.bbox[stride][i*4], out.bbox[stride][i*4+1] = 0.5, 0.5
	out.bbox[stride][i*4+2], out.bbox[stride][i*4+3] = float32(math.Log(w/float64(stride))), float32(math.Log(h/float64(stride)))
	for k := range 5 { // landmarks 1 cell to the right of the cell for every point
		out.kps[stride][i*10+2*k], out.kps[stride][i*10+2*k+1] = 1, 0
	}
}

func TestDecodeYuNet_DecodesBoxLandmarksAndScore(t *testing.T) {
	out := yunetFixture(64)
	put(out, 8, 8, 2, 3, 0.81, 16, 24) // cell centre (3.5*8, 2.5*8) = (28, 20)

	dets, err := decodeYuNet(out, 64, 0.6, 0.3)
	require.NoError(t, err)
	require.Len(t, dets, 1)
	d := dets[0]
	assert.InDelta(t, 0.81, d.score, 1e-5, "score is sqrt(cls*obj)")
	assert.InDelta(t, 20, d.x1, 1e-3)
	assert.InDelta(t, 36, d.x2, 1e-3)
	assert.InDelta(t, 8, d.y1, 1e-3)
	assert.InDelta(t, 32, d.y2, 1e-3)
	assert.InDelta(t, (1+3)*8, d.landmarks[0][0], 1e-3, "landmark offsets are in cells")
	assert.InDelta(t, 2*8, d.landmarks[0][1], 1e-3)
}

func TestDecodeYuNet_DropsLowScoresAndOverlappingDuplicates(t *testing.T) {
	out := yunetFixture(64)
	put(out, 8, 8, 2, 3, 0.9, 16, 16)
	put(out, 16, 4, 1, 1, 0.7, 18, 18) // same face seen at a coarser scale: centre (24, 24) vs (28, 20)
	put(out, 8, 8, 6, 6, 0.3, 16, 16)  // below the minimum score
	put(out, 8, 8, 6, 0, 0.8, 8, 8)    // another face far away

	dets, err := decodeYuNet(out, 64, 0.6, 0.3)
	require.NoError(t, err)
	require.Len(t, dets, 2)
	assert.Greater(t, dets[0].score, dets[1].score, "best first")
	assert.InDelta(t, 0.9, dets[0].score, 1e-5)
	assert.InDelta(t, 0.8, dets[1].score, 1e-5)
}

func TestDecodeYuNet_RejectsOutputsOfTheWrongSize(t *testing.T) {
	out := yunetFixture(64)
	out.cls[8] = out.cls[8][:10]
	_, err := decodeYuNet(out, 64, 0.6, 0.3)
	assert.Error(t, err)
}

func TestFitSimilarity_RecoversRotationScaleAndTranslation(t *testing.T) {
	const scale, angle = 1.7, 0.3
	rot := complex(scale*math.Cos(angle), scale*math.Sin(angle))
	shift := complex(15.0, -8.0)
	var src [5][2]float64
	for i, p := range sfaceTemplate {
		q := (complex(p[0], p[1]) - shift) / rot // the point that maps onto template point p
		src[i] = [2]float64{real(q), imag(q)}
	}
	tr := fitSimilarity(src, sfaceTemplate)
	assert.InDelta(t, real(rot), real(tr.a), 1e-9)
	assert.InDelta(t, imag(rot), imag(tr.a), 1e-9)
	assert.InDelta(t, real(shift), real(tr.b), 1e-9)
	assert.InDelta(t, imag(shift), imag(tr.b), 1e-9)
}

func TestAlignFace_SamplesTheSourceWhereTheLandmarksAre(t *testing.T) {
	// A source image whose red channel is its x coordinate and green its y coordinate, and a face
	// whose landmarks are the template magnified by 2 (so the face is a 224×224 region).
	const side = 224
	m := rgb{w: side, h: side, step: 1, pix: make([]float32, side*side*3)}
	for y := range side {
		for x := range side {
			m.pix[(y*side+x)*3], m.pix[(y*side+x)*3+1] = float32(x), float32(y)
		}
	}
	var d detection
	for i, p := range sfaceTemplate {
		d.landmarks[i] = [2]float32{float32(p[0] * 2), float32(p[1] * 2)}
	}

	out := alignFace(m, d, 1)
	plane := sfaceSize * sfaceSize
	at := func(c, x, y int) float32 { return out[c*plane+y*sfaceSize+x] }
	assert.InDelta(t, 2*20, at(0, 20, 30), 0.01, "output pixel (20,30) shows source x = 40")
	assert.InDelta(t, 2*30, at(1, 20, 30), 0.01, "and source y = 60")
	assert.InDelta(t, 2*100, at(0, 100, 5), 0.01)
}

func TestToRGB_ReducesLargeImagesByBoxAveraging(t *testing.T) {
	img := solid(8, 4, 200, 100, 0)
	m := toRGB(img, 4) // step 2
	assert.Equal(t, 4, m.w)
	assert.Equal(t, 2, m.h)
	assert.Equal(t, 2, m.step)
	assert.InDelta(t, 200, m.pix[0], 0.5)
	assert.InDelta(t, 100, m.pix[1], 0.5)
	assert.InDelta(t, 0, m.pix[2], 0.5)
}

func solid(w, h int, r, g, b uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}
