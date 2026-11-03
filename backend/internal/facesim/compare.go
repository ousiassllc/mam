package facesim

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // the decoders of the pictures Compare takes
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// SameFaceThreshold is the cosine similarity at or above which two SFace embeddings are taken to be the same person: the threshold of the
// SFace of the OpenCV Zoo (`_threshold_cosine = 0.363` in models/face_recognition_sface/sface.py, which it found on its own face
// verification set). It is a reading aid for the numbers of a measurement, not a guarantee: a pair on the other side of it is not
// proof that the person is or is not the same.
const SameFaceThreshold = 0.363

// Comparison is the answer of Compare: the cosine similarity of the largest faces of the two pictures, or which of the two has no face.
type Comparison struct {
	// Cosine is the similarity (-1 .. 1); it means nothing unless Missing is BothHaveFaces.
	Cosine float64
	// Missing says which picture has no face (BothHaveFaces: none is missing).
	Missing Side
}

// Compare measures how much the face of generated is the face of reference. Both are image files (PNG, JPEG or WebP): the largest face of
// each is compared (Similarity). A picture that does not decode, or an engine that fails, is the error; a picture without a face is not (it is
// Comparison.Missing).
func Compare(ctx context.Context, eng Engine, reference, generated []byte) (Comparison, error) {
	ref, _, err := image.Decode(bytes.NewReader(reference))
	if err != nil {
		return Comparison{}, fmt.Errorf("facesim: decode the reference: %w", err)
	}
	gen, _, err := image.Decode(bytes.NewReader(generated))
	if err != nil {
		return Comparison{}, fmt.Errorf("facesim: decode the generated image: %w", err)
	}
	cos, missing, err := Similarity(ctx, eng, ref, gen)
	if err != nil {
		return Comparison{}, err
	}
	return Comparison{Cosine: cos, Missing: missing}, nil
}
