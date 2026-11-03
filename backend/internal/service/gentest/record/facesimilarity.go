package record

// The reasons a face similarity could not be measured (FaceSimilarity.Skipped).
const (
	// SkipReferenceNoFace: the reference of the role "face" shows no face the detector finds.
	SkipReferenceNoFace = "reference_no_face"
	// SkipResultNoFace: the made picture shows no face the detector finds (a swap that failed, a face too small or turned away).
	SkipResultNoFace = "result_no_face"
	// SkipError: the measurement failed (a picture that does not decode, the engine failing); the result itself is not affected.
	SkipError = "error"
)

// MetricSFace names the metric of FaceSimilarity.
const MetricSFace = "sface"

// FaceSimilarity is how much the face of a result is the face of the reference the batch gave it with the role "face"
// (gentest_result.params "face_similarity"; ResultView.FaceSimilarity). It is measured when the result is made, from the largest face of
// each picture, with SFace (the facesim package, Apache-2.0, run on this PC): the cosine similarity of the two embeddings. Absent when
// the result had no reference of the role "face" or the face models are not installed.
type FaceSimilarity struct {
	// Metric is the embedding the number is of (MetricSFace).
	Metric string `json:"metric" jsonschema:"顔の埋め込みの種類（sface = OpenCV Zoo の SFace。旧指標の InsightFace buffalo_l とは数値が違う）"`
	// Cosine is the similarity (-1 .. 1); meaningless when Skipped is set.
	Cosine float64 `json:"cosine" jsonschema:"参照の顔と出力の顔の埋め込みのコサイン類似度（-1〜1。Skipped があるときは意味が無い）"`
	// Threshold is the cosine at or above which SFace takes two faces to be the same person (facesim.SameFaceThreshold); SamePerson is
	// Cosine >= Threshold.
	Threshold  float64 `json:"threshold" jsonschema:"SFace が同じ人と見なす閾値（0.363。OpenCV Zoo の既定）"`
	SamePerson bool    `json:"same_person" jsonschema:"cosine が threshold 以上か。目安であって、同じ人である保証ではない"`
	// Skipped says why no number was made (the Skip constants); empty when there is one.
	Skipped string `json:"skipped,omitempty" jsonschema:"測れなかった理由: reference_no_face / result_no_face / error"`
}
