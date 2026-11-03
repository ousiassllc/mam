package record

// FaceSwap is what a result made by "swap the face" remembers (gentest_result.params "face_swap"; ResultView.FaceSwap): the result whose
// picture the face was put into and the models FaceFusion ran with. The result is a file of its own: deleting the source leaves it
// (SourceResultID then points to nothing). The picture of the face is the input of the role "face" of the result.
type FaceSwap struct {
	// SourceResultID is the result whose picture was swapped into; "" when the picture was not one of the history's.
	SourceResultID string `json:"source_result_id,omitempty" jsonschema:"顔を入れ替えた元の結果の result_id（削除されていれば get_generation_result は見つからない）"`
	Swapper        string `json:"swapper" jsonschema:"FaceFusion の入れ替えモデル（--face-swapper-model）"`
	Detector       string `json:"detector" jsonschema:"FaceFusion の顔検出モデル（--face-detector-model）"`
	// Enhancer is the face restoration model; "" when none ran.
	Enhancer string `json:"enhancer,omitempty" jsonschema:"FaceFusion の顔の復元モデル（--face-enhancer-model）。空なら復元しなかった"`
	// Warnings are the license cautions of the models the user chose (non-commercial, research only, commercial use unknown), in words.
	Warnings []string `json:"warnings,omitempty" jsonschema:"選んだモデルのライセンス上の注意（商用NG・研究用・商用不明）"`
}
