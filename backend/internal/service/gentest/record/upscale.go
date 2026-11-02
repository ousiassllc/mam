package record

// Upscale is what a result made by "make it larger" remembers: the result it enlarged and the values it used (gentest_result.params
// "upscale"; ResultView.Upscale). The result is a file of its own: deleting the source leaves it (SourceResultID then points to nothing).
type Upscale struct {
	SourceResultID string `json:"source_result_id" jsonschema:"大きくした元の結果の result_id（削除されていれば get_generation_result は見つからない）"`
	SourceModel    string `json:"source_model,omitempty" jsonschema:"元の結果を作ったモデル"`
	Scale          int    `json:"scale" jsonschema:"倍率（2 か 4）"`
	// Model is the upscale workflow that ran, Mode how it enlarges (model / two_pass) and Native the factor of its upscale model.
	Model  string `json:"model" jsonschema:"使ったアップスケールのモデル識別子"`
	Mode   string `json:"mode" jsonschema:"model（アップスケール用モデルだけ）/ two_pass（そのあと低い denoise でもう一度サンプリングして仕上げた）"`
	Native int    `json:"native"`
	// SourceWidth × SourceHeight and Width × Height are the size of the source and of the result in pixels.
	SourceWidth  int `json:"source_width"`
	SourceHeight int `json:"source_height"`
	Width        int `json:"width" jsonschema:"出力の予定の幅（元の幅 × 倍率）"`
	Height       int `json:"height" jsonschema:"出力の予定の高さ（元の高さ × 倍率）"`
}
