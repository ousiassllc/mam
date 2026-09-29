// Package facesim measures face identity locally (spec/spec.md §12.4): it detects the faces of an
// image and turns each into an embedding whose cosine similarity to a reference face says whether
// it is the same person. Nothing leaves the machine: the models run in-process through the ONNX
// Runtime shared library (loaded at run time, so the binary stays a single CGO-free executable).
//
// The default models are YuNet (face detection, MIT) and SFace (face embedding, Apache-2.0) of the
// OpenCV Zoo, see spec/architecture/model-adapters.md §8.
package facesim

import (
	"context"
	"errors"
	"image"
	"math"
)

// ErrUnavailable means the engine cannot run: the models or the ONNX Runtime library are missing.
var ErrUnavailable = errors.New("facesim: the face models or the ONNX Runtime library are not available")

// Embedding is an L2-normalised face embedding: the cosine similarity of two Embeddings is their
// dot product.
type Embedding []float32

// Face is one detected face with its embedding. Box is the pixel rectangle of the image.
type Face struct {
	Box       image.Rectangle
	Score     float32
	Embedding Embedding
}

// Engine finds the faces of an image. Implementations must be safe for concurrent use.
type Engine interface {
	// Faces returns every face of img whose detection confidence reaches the engine's minimum,
	// largest first. An image without a face returns no faces and no error.
	Faces(ctx context.Context, img image.Image) ([]Face, error)
}

// Cosine is the cosine similarity of two embeddings (-1 .. 1). Embeddings of different lengths
// or of zero length have similarity 0.
func Cosine(a, b Embedding) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// normalise scales v to unit length in place (a zero vector stays zero).
func normalise(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}
