package record

// VideoEnhance is what a result made by "make a video larger" or "make a video smoother" remembers: the result it started from, what was
// done and the values (gentest_result.params "video_enhance"; ResultView.VideoEnhance). The result is a file of its own: deleting the
// source leaves it (SourceResultID then points to nothing).
type VideoEnhance struct {
	SourceResultID string `json:"source_result_id" jsonschema:"元の動画の結果の result_id（削除されていれば get_generation_result は見つからない）"`
	SourceModel    string `json:"source_model,omitempty" jsonschema:"元の結果を作ったモデル"`
	Op             string `json:"op" jsonschema:"upscale（大きくした）/ interpolate（コマを増やした）"`
	Provider       string `json:"provider" jsonschema:"local（この PC の ComfyUI）/ topaz（Topaz Video API。有料クラウド）"`
	Model          string `json:"model" jsonschema:"使ったモデルの識別子（ローカルはワークフロー名、Topaz は Topaz のモデル名）"`
	// Scale and TargetShortSide (upscale); Multiplier and TargetFPS (interpolate): what was asked. A field that does not count is 0.
	Scale           float64 `json:"scale,omitempty" jsonschema:"アップスケールの倍率"`
	TargetShortSide int     `json:"target_short_side,omitempty" jsonschema:"アップスケールで短辺をそろえた長さ（720・1080 など）"`
	Multiplier      int     `json:"multiplier,omitempty" jsonschema:"コマ補間の倍率（2・4）"`
	TargetFPS       float64 `json:"target_fps,omitempty" jsonschema:"コマ補間で指定した出力の fps"`
	// The source and the output as they are (the output ones are the planned values until the result is read back).
	SourceWidth  int     `json:"source_width"`
	SourceHeight int     `json:"source_height"`
	SourceFPS    float64 `json:"source_fps"`
	SourceFrames int     `json:"source_frames"`
	Width        int     `json:"width" jsonschema:"出力の予定の幅"`
	Height       int     `json:"height" jsonschema:"出力の予定の高さ"`
	FPS          float64 `json:"fps" jsonschema:"出力の予定の fps"`
	// Topaz is set when Provider is topaz.
	Topaz *TopazRun `json:"topaz,omitempty"`
}

// TopazRun is what a result made at Topaz remembers. It never holds the API key.
type TopazRun struct {
	RequestID        string  `json:"request_id" jsonschema:"Topaz のリクエスト ID（問い合わせ用）"`
	EstimatedCredits float64 `json:"estimated_credits" jsonschema:"見積りのクレジット（上限。利用者が確認した額）"`
	Attribution      string  `json:"attribution" jsonschema:"表示が必要な出典（Powered by Topaz Labs）"`
}
