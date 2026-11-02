package facesim

import (
	"context"
	"image"
)

// Side says which of the two images of Similarity has no face.
type Side int

// The sides of a comparison.
const (
	// BothHaveFaces: nothing is missing.
	BothHaveFaces Side = iota
	// FirstHasNoFace: the first image (the reference) shows no face.
	FirstHasNoFace
	// SecondHasNoFace: the second image shows no face.
	SecondHasNoFace
)

// Similarity is the cosine similarity (-1 .. 1) of the largest face of a and the largest face of b: the question "is the face of b
// the face of a" for two pictures. missing says which image has no face (the similarity is then 0 and means nothing); an error is the
// engine failing. Only the largest face counts, as it does for a reference image of a Character.
func Similarity(ctx context.Context, e Engine, a, b image.Image) (similarity float64, missing Side, err error) {
	fa, err := e.Faces(ctx, a)
	if err != nil {
		return 0, BothHaveFaces, err
	}
	if len(fa) == 0 {
		return 0, FirstHasNoFace, nil
	}
	fb, err := e.Faces(ctx, b)
	if err != nil {
		return 0, BothHaveFaces, err
	}
	if len(fb) == 0 {
		return 0, SecondHasNoFace, nil
	}
	return Cosine(fa[0].Embedding, fb[0].Embedding), BothHaveFaces, nil
}
