package record

import (
	"math"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// FaceSummary is how well the face held over the results of one model of a batch that were made from a reference of the role "face" (Issue #998): the
// SFace similarities of those results (FaceSimilarity) read together, so that the effect of a setting (the strength of the reference, the model, a prompt) is a
// number to compare between two batches and not an impression of a grid of pictures. The same metric for every model (SFace), so the means are comparable.
type FaceSummary struct {
	Model      string `json:"model" jsonschema:"正規のモデル識別子"`
	ModelLabel string `json:"model_label"`
	// Measured is the number of results that got a number; Skipped the number that had a face reference but no number (no face found, a failure,
	// or the face similarity off: see FaceSimilarity.Skipped of each result). They do not count in the figures below.
	Measured int `json:"measured" jsonschema:"類似度の数値が付いた結果の数"`
	Skipped  int `json:"skipped" jsonschema:"役割 face の参照があったが数値が付かなかった結果の数（顔が見つからない・測定の失敗・顔の類似度がオフ。理由は各結果の face_similarity.skipped）"`
	// MeanCosine, MinCosine and MaxCosine are over the Measured results; 0 when none was measured.
	MeanCosine float64 `json:"mean_cosine" jsonschema:"測れた結果のコサイン類似度の平均（SFace。測れた結果が 0 件なら 0）"`
	MinCosine  float64 `json:"min_cosine"`
	MaxCosine  float64 `json:"max_cosine"`
	// SamePerson is how many of the Measured results are at or above the threshold; SamePersonRate is that over Measured.
	SamePerson     int     `json:"same_person" jsonschema:"threshold 以上（同じ人と見なす目安）の結果の数"`
	SamePersonRate float64 `json:"same_person_rate" jsonschema:"same_person / measured（0〜1）"`
	Threshold      float64 `json:"threshold" jsonschema:"SFace が同じ人と見なす閾値（0.363。目安であって保証ではない）"`
}

// SummarizeFaces reads the face similarities of results together, one FaceSummary per model in the order the models first appear. Results without a face
// similarity (no reference of the role "face") are not part of it, so a batch without any gets none (nil).
func SummarizeFaces(results []ResultView) []FaceSummary {
	var out []FaceSummary
	at := map[string]int{}
	sums := map[string]float64{}
	for _, r := range results {
		if r.FaceSimilarity == nil {
			continue
		}
		i, ok := at[r.Model]
		if !ok {
			i = len(out)
			at[r.Model] = i
			out = append(out, FaceSummary{Model: r.Model, ModelLabel: r.ModelLabel, MinCosine: math.Inf(1), MaxCosine: math.Inf(-1), Threshold: facesim.SameFaceThreshold})
		}
		s, f := &out[i], r.FaceSimilarity
		if f.Skipped != "" {
			s.Skipped++
			continue
		}
		s.Measured++
		sums[r.Model] += f.Cosine
		s.MinCosine, s.MaxCosine = min(s.MinCosine, f.Cosine), max(s.MaxCosine, f.Cosine)
		if f.SamePerson {
			s.SamePerson++
		}
	}
	for i := range out {
		s := &out[i]
		if s.Measured == 0 {
			s.MinCosine, s.MaxCosine = 0, 0
			continue
		}
		s.MeanCosine = sums[s.Model] / float64(s.Measured)
		s.SamePersonRate = float64(s.SamePerson) / float64(s.Measured)
	}
	return out
}
