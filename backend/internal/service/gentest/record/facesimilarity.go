package record

import (
	"slices"

	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
)

// The reasons a face similarity could not be measured (FaceSimilarity.Skipped).
const (
	// SkipReferenceNoFace: the reference of the role "face" shows no face the detector finds.
	SkipReferenceNoFace = "reference_no_face"
	// SkipResultNoFace: the made picture shows no face the detector finds (a swap that failed, a face too small or turned away).
	SkipResultNoFace = "result_no_face"
	// SkipError: the measurement failed (a picture that does not decode, the engine failing); the result itself is not affected.
	SkipError = "error"
	// SkipModelsMissing: the face models (YuNet, SFace) are not in the model folder, so the face similarity is off (#1026). Reason and Fix say what to do: install the face models from the model manager's "顔チェック用モデル".
	SkipModelsMissing = "models_missing"
	// SkipRuntimeMissing: the models are there but the ONNX Runtime library is missing or does not load, so the face similarity is off (#1026).
	SkipRuntimeMissing = "runtime_unavailable"
)

// MetricSFace names the metric of FaceSimilarity.
const MetricSFace = "sface"

// FaceSimilarity is how much the face of a result is the face of the reference the batch gave it with the role "face"
// (gentest_result.params "face_similarity"; ResultView.FaceSimilarity). It is measured when the result is made, from the largest face of
// each picture, with SFace (the facesim package, Apache-2.0, run on this PC): the cosine similarity of the two embeddings. Absent when
// the result had no reference of the role "face"; when the face models or the ONNX Runtime library are not there it is present with Skipped
// models_missing / runtime_unavailable, so that "off" is read from the result (#1026).
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
	Skipped string `json:"skipped,omitempty" jsonschema:"測れなかった理由: reference_no_face（参照に顔が無い）/ result_no_face（出力に顔が無い）/ error（測定の失敗）/ models_missing（顔モデルが無い＝顔の類似度がオフ）/ runtime_unavailable（ONNX Runtime が使えない＝オフ）"`
	// Reason and Fix are the words for models_missing / runtime_unavailable: why it is off and how to turn it on (the apperr texts, in Japanese): install the models from the model
	// manager for models_missing, provide the ONNX Runtime library for runtime_unavailable (installing the models does not fix that).
	Reason string `json:"reason,omitempty" jsonschema:"models_missing / runtime_unavailable のとき: なぜ測れないか（日本語）"`
	Fix    string `json:"fix,omitempty" jsonschema:"models_missing / runtime_unavailable のとき: 直し方（日本語）。models_missing はモデルマネージャーの「顔チェック用モデル」から導入する（install_face_models。再起動は要らない）。runtime_unavailable は ONNX Runtime のライブラリを用意する（モデルを入れ直しても直らない）"`
	// ModelDir is the folder the face models are looked up in; MissingFiles are the ones that are not there (models_missing).
	ModelDir     string   `json:"model_dir,omitempty" jsonschema:"顔モデルを探すフォルダ（models_missing のとき）"`
	MissingFiles []string `json:"missing_files,omitempty" jsonschema:"無い顔モデルのファイル名（models_missing のとき）"`
}

// FaceSimilarityStatus is whether the face similarity is measured at all, for the tools that say it before a batch runs (list_generation_models). It is the
// state of the application, not of a result: Enabled is false while the face models or the ONNX Runtime library are not there, and Off is what a result with a
// face reference then records (FaceSimilarity).
type FaceSimilarityStatus struct {
	Enabled bool `json:"enabled" jsonschema:"顔の類似度（SFace）を測れる。false の間、役割 face の参照を付けた結果は face_similarity.skipped に理由が残る"`
	// Off is what a result with a face reference records while it is off (nil while Enabled).
	Off *FaceSimilarity `json:"off,omitempty" jsonschema:"オフのとき、結果に残る内容（skipped・reason・fix・model_dir・missing_files）"`
}

// StatusOf is the status of a service that measures (scoring) or, when it does not, records off (nil in a service without the face similarity at all).
func StatusOf(scoring bool, off *FaceSimilarity) FaceSimilarityStatus {
	return FaceSimilarityStatus{Enabled: scoring, Off: off}
}

// WarnFaceSimilarityOff is the code of the warning that a request has a reference of the role "face" and the face similarity is off, so its results get no number.
const WarnFaceSimilarityOff = "face_similarity_off"

// FaceOffNote is the warning of the confirmation for a request whose reference roles include "face" while the face similarity is off (not scoring, and off says
// why): its results carry skipped models_missing / runtime_unavailable and no number (#1026). ok is false when there is nothing to say.
func FaceOffNote(scoring bool, off *FaceSimilarity, roles []string) (code, message string, ok bool) {
	if scoring || off == nil || !slices.Contains(roles, gentestrepo.RoleFace) {
		return "", "", false
	}
	return WarnFaceSimilarityOff, "顔の類似度（SFace）はオフです: " + off.Reason + " " + off.Fix + "（生成はできますが、結果に類似度の数値は付きません）", true
}
