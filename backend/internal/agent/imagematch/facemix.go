package imagematch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // the types Prepare leaves as they are, and JPEG
	_ "image/png"
	"math"

	_ "golang.org/x/image/webp" // Prepare turns WebP into JPEG, but the decoder is cheap to have

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// FaceMix is how the face of the generated image is mixed into the similarity (#964): the points of the LLM (0..100) and the points of
// the face (the cosine similarity of the SFace embeddings of the largest face of both images, mapped to 0..100 by Floor and Ceil) are
// averaged with Weight on the face. Everything that decides the mix is here, so that the choice of the face model (#960) changes this
// one place: the weight and the mapping were measured for SFace, and another embedding has another range of similarities.
//
// Weight 0 is off, and it is the default: the training data of the SFace weights is not stated by their distribution
// (spec/architecture/model-adapters.md §8), so the face is not used for the score until the operator turns it on.
type FaceMix struct {
	// Weight is the share of the face points in the score, 0 (off) .. 1.
	Weight float64 `json:"weight"`
	// Floor is the cosine similarity that is 0 points; Ceil the one that is 100 points (linear between, clamped outside). The
	// same-person threshold of SFace in the OpenCV Zoo is 0.363, which these defaults map to 33 points.
	Floor float64 `json:"floor"`
	Ceil  float64 `json:"ceil"`
}

// DefaultFaceMix is the face mix that is not on.
func DefaultFaceMix() FaceMix { return FaceMix{Weight: 0, Floor: 0.2, Ceil: 0.7} }

// UnmarshalJSON reads the settings over the defaults: a key the file leaves out keeps its default, and an unknown key is an error
// (as everywhere in the settings files).
func (m *FaceMix) UnmarshalJSON(data []byte) error {
	type plain FaceMix
	v := plain(DefaultFaceMix())
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return err
	}
	*m = FaceMix(v)
	return nil
}

// Validate checks that the numbers are usable.
func (m FaceMix) Validate() error {
	var errs []error
	if math.IsNaN(m.Weight) || m.Weight < 0 || m.Weight > 1 {
		errs = append(errs, errors.New("weight must be in 0..1"))
	}
	if math.IsNaN(m.Floor) || math.IsNaN(m.Ceil) || m.Floor < -1 || m.Ceil > 1 || m.Floor >= m.Ceil {
		errs = append(errs, errors.New("need -1 <= floor < ceil <= 1"))
	}
	return errors.Join(errs...)
}

// Enabled says whether the face is to be mixed in at all.
func (m FaceMix) Enabled() bool { return m.Weight > 0 }

// Points maps a cosine similarity to 0..100.
func (m FaceMix) Points(cosine float64) float64 {
	return math.Max(0, math.Min(100, (cosine-m.Floor)/(m.Ceil-m.Floor)*100))
}

// Mix is the score of the LLM and of the face together: the average with the weight of the face, rounded to an integer.
func (m FaceMix) Mix(llm, face float64) int {
	return int(math.Round((1-m.Weight)*llm + m.Weight*face))
}

// Why the face was left out of a score that was to have it.
const (
	// SkipUnavailable: the face models or the ONNX Runtime library are not available.
	SkipUnavailable = "unavailable"
	// SkipReferenceNoFace: the reference image shows no face (a landscape, a thing): there is no face to keep.
	SkipReferenceNoFace = "reference_no_face"
	// SkipGeneratedNoFace: no face was found in the generated image.
	SkipGeneratedNoFace = "generated_no_face"
	// SkipError: the face engine failed on one of the pictures.
	SkipError = "error"
)

// FacePart is the face's share of a score: the cosine similarity, the points it is worth and the weight it got.
type FacePart struct {
	Cosine float64 `json:"cosine"`
	Points int     `json:"points"`
	Weight float64 `json:"weight"`
}

// Scoring is how a score came about when the face is to be mixed in: LLM is what the LLM said, Face the share of the face. A score
// without Face is the LLM's alone, and Skipped says why (the Skip constants).
type Scoring struct {
	LLM     int       `json:"llm"`
	Face    *FacePart `json:"face,omitempty"`
	Skipped string    `json:"face_skipped,omitempty"`
}

// UseFaces makes the Matcher mix the face of the generated image into the similarity with mix, measured by engine. A nil engine with a
// mix that is on is the face models missing: the scores are the LLM's alone and say so (SkipUnavailable).
func (m *Matcher) UseFaces(engine facesim.Engine, mix FaceMix) *Matcher {
	m.faces, m.mix = engine, mix
	return m
}

// score is the score of a comparison the LLM scored llm points: mixed with the face when that is on and both pictures have one,
// else the LLM's alone. It is nil, nil when the face is off.
func (m *Matcher) score(ctx context.Context, reference, generated Image, llm int) (int, *Scoring) {
	if !m.mix.Enabled() {
		return llm, nil
	}
	sc := &Scoring{LLM: llm}
	if !facesim.Usable(m.faces) { // nil, or an engine whose models are not installed yet (they can be while mam runs, #1026)
		sc.Skipped = SkipUnavailable
		return llm, sc
	}
	cosine, missing, err := m.similarity(ctx, reference, generated)
	switch {
	case err != nil:
		sc.Skipped = SkipError
	case missing == facesim.FirstHasNoFace:
		sc.Skipped = SkipReferenceNoFace
	case missing == facesim.SecondHasNoFace:
		sc.Skipped = SkipGeneratedNoFace
	default:
		points := m.mix.Points(cosine)
		sc.Face = &FacePart{Cosine: math.Round(cosine*1e4) / 1e4, Points: int(math.Round(points)), Weight: m.mix.Weight}
		return m.mix.Mix(float64(llm), points), sc
	}
	return llm, sc
}

func (m *Matcher) similarity(ctx context.Context, reference, generated Image) (float64, facesim.Side, error) {
	ref, _, err := image.Decode(bytes.NewReader(reference.Data))
	if err != nil {
		return 0, facesim.BothHaveFaces, fmt.Errorf("decode the reference: %w", err)
	}
	gen, _, err := image.Decode(bytes.NewReader(generated.Data))
	if err != nil {
		return 0, facesim.BothHaveFaces, fmt.Errorf("decode the generated image: %w", err)
	}
	return facesim.Similarity(ctx, m.faces, ref, gen)
}
