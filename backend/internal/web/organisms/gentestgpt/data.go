// Package gentestgpt draws the "GPT Image 2.6 の設定" of the generation test form (Issue #1096): the complexity, the output format (PNG / GIF / SVG) and the
// frames of a GIF. Only GPT Image 2.6 Flare and Sunburst (API key) take them; the section follows the models that are ticked
// (static/src/components/gentest/gpt-image.ts) and says, for a ticked model that does not take them, why. The handlers are web/handler/gentest/gptform.
package gentestgpt

import "strconv"

// Form field names of the settings (the handlers read the same names).
const (
	FieldComplexity    = "gpt_complexity"
	FieldFormat        = "gpt_format"
	FieldFrameCount    = "gpt_frame_count"
	FieldFrameDuration = "gpt_frame_duration_ms"
)

// FormatGIF and FormatSVG are the values of the select of the output format ("" is PNG).
const (
	FormatGIF = "gif"
	FormatSVG = "svg"
)

// Data is the section as the form draws it.
type Data struct {
	// Offered says a model of the form takes these settings or is refused them: the section is in the page (it starts hidden unless Shown).
	Offered bool
	// Shown says the section starts visible: a ticked model takes them or is refused them, or a value was given. Fields says its fields start visible
	// (a ticked model takes them, or a value is given: a value that a model cannot take stays in view, so that it can be taken out).
	Shown, Fields bool
	// Complexity, Format, FrameCount and FrameDuration are what the form holds ("" when not given).
	Complexity, Format, FrameCount, FrameDuration string
	// Complexities and Formats are what the models take (modelcatalog.Info.GPTImage); the ranges are those of a GIF's frames.
	Complexities, Formats              []string
	MinFrames, MaxFrames, MinMS, MaxMS int
	// Notes are the lines for the ticked models that do not take the settings: the model and why.
	Notes []string
}

// GIF says the frames of a GIF are offered now.
func (d Data) GIF() bool { return d.Format == FormatGIF }

// Offers says the output format is one the models take.
func (d Data) Offers(format string) bool {
	for _, f := range d.Formats {
		if f == format {
			return true
		}
	}
	return false
}

func itoa(n int) string { return strconv.Itoa(n) }

// formatLabel is a format as the select says it.
func formatLabel(format string) string {
	switch format {
	case FormatGIF:
		return "GIF（アニメーション）"
	case FormatSVG:
		return "SVG（ベクター）"
	}
	return "PNG（既定）"
}

// Hints of the section (the script shows the one of the format that is chosen).
const (
	GIFHint    = "GIF はアカウントのプレビュー利用が必要です。使えないアカウントでは OpenAI のエラーをそのまま表示します"
	SVGHint    = "新規の生成だけ（入力画像があると使えません）。費用は見積れません"
	MarkupHint = "マークアップ（入力画像の 1 枚目に重ねる透明な PNG）は、入力画像の役割で「マークアップ」を選びます。入力画像（編集対象）も付けてください"
)
