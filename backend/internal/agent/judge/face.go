package judge

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // decoders of reference and frame images
	_ "image/png"
	"math"
	"sort"

	_ "golang.org/x/image/webp" // generated images may be WebP

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// FaceSubject is a Character whose face is measured: the reference images that show it.
type FaceSubject struct {
	ID              string
	ReferenceImages [][]byte
}

// FaceReport is the outcome of the Face Identity Similarity measurement of one Take.
type FaceReport struct {
	Metrics []domain.FaceIdentityMetric
	Issues  []domain.JudgeIssue
}

// FaceMeasurer measures Face Identity Similarity locally (spec/spec.md §12.4): sampled frames →
// face detection → face embedding → cosine similarity to the reference faces. Nothing is sent
// anywhere.
type FaceMeasurer struct {
	engine facesim.Engine
	cfg    FaceConfig
}

// NewFaceMeasurer returns a FaceMeasurer that runs on engine.
func NewFaceMeasurer(engine facesim.Engine, cfg FaceConfig) *FaceMeasurer {
	return &FaceMeasurer{engine: engine, cfg: cfg}
}

// subjectState is what the measurement knows of one Character.
type subjectState struct {
	id         string
	refs       []facesim.Embedding
	warn, fail float64
	calibrated bool
	// perFrame[i] is the similarity in frame i, or NaN when the Character's face was not found.
	perFrame []float64
}

// Measure measures every subject over the clip. A subject none of whose reference images shows a
// detectable face is left out of the report (nothing to compare with).
func (m *FaceMeasurer) Measure(ctx context.Context, subjects []FaceSubject, clip Clip) (FaceReport, error) {
	var states []*subjectState
	for _, s := range subjects {
		st, err := m.reference(ctx, s)
		if err != nil {
			return FaceReport{}, err
		}
		if st != nil {
			states = append(states, st)
		}
	}
	if len(states) == 0 {
		return FaceReport{}, nil
	}
	for _, st := range states {
		st.perFrame = make([]float64, len(clip.Frames))
		for i := range st.perFrame {
			st.perFrame[i] = math.NaN()
		}
	}
	for i, fr := range clip.Frames {
		img, _, err := image.Decode(bytes.NewReader(fr.JPEG))
		if err != nil {
			return FaceReport{}, fmt.Errorf("decode frame %d: %w", i, err)
		}
		faces, err := m.engine.Faces(ctx, img)
		if err != nil {
			return FaceReport{}, err
		}
		assign(states, faces, i)
	}

	var rep FaceReport
	for _, st := range states {
		metric := m.metric(st, len(clip.Frames))
		rep.Metrics = append(rep.Metrics, metric)
		if metric.Evaluated {
			rep.Issues = append(rep.Issues, m.driftIssues(st, clip)...)
		}
	}
	return rep, nil
}

// reference embeds the reference images of the subject and calibrates its thresholds. It returns
// nil when no reference image shows a face.
func (m *FaceMeasurer) reference(ctx context.Context, s FaceSubject) (*subjectState, error) {
	st := &subjectState{id: s.ID}
	for _, data := range s.ReferenceImages {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			continue // an unreadable reference image is skipped; the others still count
		}
		faces, err := m.engine.Faces(ctx, img)
		if err != nil {
			return nil, err
		}
		if len(faces) > 0 {
			st.refs = append(st.refs, faces[0].Embedding) // the largest face is the subject
		}
	}
	if len(st.refs) == 0 {
		return nil, nil
	}
	st.warn, st.fail = m.cfg.WarnSimilarity, m.cfg.FailSimilarity
	if mean, ok := meanPairwise(st.refs); ok && mean >= m.cfg.MinCalibrationSimilarity {
		st.fail = m.cfg.CalibrationFailScale * mean
		st.warn = max(m.cfg.CalibrationWarnScale*mean, st.fail)
		st.calibrated = true
	}
	return st, nil
}

// meanPairwise is the mean cosine similarity over all pairs of the embeddings (false below two).
func meanPairwise(embs []facesim.Embedding) (float64, bool) {
	var sum float64
	var n int
	for i := range embs {
		for j := i + 1; j < len(embs); j++ {
			sum += facesim.Cosine(embs[i], embs[j])
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// assign gives each face of frame i to the subject it resembles most, best pairs first, one face
// per subject and one subject per face. Subjects left without a face are missing in this frame.
func assign(states []*subjectState, faces []facesim.Face, i int) {
	type pair struct {
		subject, face int
		sim           float64
	}
	var pairs []pair
	for si, st := range states {
		for fi, f := range faces {
			best := math.Inf(-1)
			for _, ref := range st.refs {
				best = max(best, facesim.Cosine(ref, f.Embedding))
			}
			pairs = append(pairs, pair{si, fi, best})
		}
	}
	sort.SliceStable(pairs, func(a, b int) bool { return pairs[a].sim > pairs[b].sim })
	usedSubject, usedFace := map[int]bool{}, map[int]bool{}
	for _, p := range pairs {
		if usedSubject[p.subject] || usedFace[p.face] {
			continue
		}
		usedSubject[p.subject], usedFace[p.face] = true, true
		states[p.subject].perFrame[i] = p.sim
	}
}

// metric summarises one subject. Frames without its face are missing values: they raise the
// undetected ratio and nothing else.
func (m *FaceMeasurer) metric(st *subjectState, sampled int) domain.FaceIdentityMetric {
	var sum float64
	detected := 0
	minSim := math.Inf(1)
	for _, v := range st.perFrame {
		if math.IsNaN(v) {
			continue
		}
		detected++
		sum += v
		minSim = min(minSim, v)
	}
	out := domain.FaceIdentityMetric{
		CharacterID: st.id, SampledFrames: sampled,
		WarnThreshold: round4(st.warn), FailThreshold: round4(st.fail), Calibrated: st.calibrated,
	}
	if sampled > 0 {
		out.UndetectedRatio = round4(float64(sampled-detected) / float64(sampled))
	}
	if detected > 0 {
		out.SimilarityMean, out.SimilarityMin = round4(sum/float64(detected)), round4(minSim)
	}
	out.Evaluated = detected >= m.cfg.MinDetectedFrames && float64(sampled-detected)/float64(sampled) <= m.cfg.MaxUndetectedRatio
	return out
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

// level classifies a similarity: 0 fine, 1 below the warning threshold, 2 below the FAIL threshold.
func (st *subjectState) level(sim float64) int {
	switch {
	case sim < st.fail:
		return 2
	case sim < st.warn:
		return 1
	}
	return 0
}

// driftIssues turns each run of consecutive sampled frames below a threshold into a
// face_identity_drift Issue. A frame without the face ends a run. A run needs MinRunFrames frames;
// it is high severity when MinRunFrames consecutive frames of it are below the FAIL threshold
// (a FAIL threshold breach), medium otherwise (a warning).
func (m *FaceMeasurer) driftIssues(st *subjectState, clip Clip) []domain.JudgeIssue {
	var issues []domain.JudgeIssue
	emit := func(start, end int) {
		frames := end - start + 1
		if frames < m.cfg.MinRunFrames {
			return
		}
		severity, failRun := domain.SeverityMedium, 0
		for i := start; i <= end; i++ {
			if st.level(st.perFrame[i]) == 2 {
				failRun++
				if failRun >= m.cfg.MinRunFrames {
					severity = domain.SeverityHigh
				}
			} else {
				failRun = 0
			}
		}
		from := clip.Frames[start].Time
		to := min(clip.Frames[end].Time+clip.Interval, clip.Duration)
		first, last := clip.FrameRange(from, to)
		issues = append(issues, domain.JudgeIssue{
			Type: domain.IssueTypeFaceIdentityDrift, Severity: severity, Dimension: domain.DimCharacterIdentity,
			TimeRange: FormatTimeRange(from, to), Frames: []int{first, last}, CharacterID: st.id,
		})
	}
	start := -1
	for i, v := range st.perFrame {
		if !math.IsNaN(v) && st.level(v) > 0 {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			emit(start, i-1)
			start = -1
		}
	}
	if start >= 0 {
		emit(start, len(st.perFrame)-1)
	}
	return issues
}

// FormatTimeRange renders seconds as the Judge Output's time range: "3.0-3.9s".
func FormatTimeRange(from, to float64) string { return fmt.Sprintf("%.1f-%.1fs", from, to) }
