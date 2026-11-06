package gptimage

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Options are the settings of one request that only GPT Image 2.6 Flare and Sunburst take through OpenAI's Image API (the guide
// https://developers.openai.com/api/docs/guides/image-generation, "GPT Image 2.6", read 2026-11-06; the field lists are those of the API
// reference: .../api/reference/resources/images/methods/generate and .../edit). The zero value sends none of them: a request is then the one of
// GPT Image 2 (spec/architecture/model-adapters.md §9.5).
type Options struct {
	// Complexity is how much the model plans before it renders (`complexity`: low / medium / high). "" omits it: OpenAI's default (high).
	Complexity string `json:"complexity,omitempty"`
	// Format is the output format that is not PNG (`output_format`): FormatGIF (generations and edits) or FormatSVG (generations only). "" is PNG.
	Format string `json:"output_format,omitempty"`
	// FrameCount and FrameDurationMS are the frames of a GIF (`frame_count` 2-16, `frame_duration_ms` 20-2000); 0 omits them (OpenAI's 9 and 200).
	FrameCount      int `json:"frame_count,omitempty"`
	FrameDurationMS int `json:"frame_duration_ms,omitempty"`
}

// The values of Options.
const (
	ComplexityLow    = "low"
	ComplexityMedium = "medium"
	ComplexityHigh   = "high"

	FormatGIF = "gif"
	FormatSVG = "svg"

	MinFrameCount      = 2
	MaxFrameCount      = 16
	MinFrameDurationMS = 20
	MaxFrameDurationMS = 2000
)

// Complexities lists the values of Options.Complexity.
func Complexities() []string { return []string{ComplexityLow, ComplexityMedium, ComplexityHigh} }

// Zero reports whether no option is set.
func (o Options) Zero() bool { return o == Options{} }

// TakesOptions reports whether the model is one of the two that take Options through the API-key path: GPT Image 2.6 Flare and Sunburst.
// GPT Image 2 does not, and neither do their ChatGPT-subscription twins (SubscriptionRefusal).
func TakesOptions(m domain.ModelName) bool {
	return m == domain.ModelGPTImage26Flare || m == domain.ModelGPTImage26Sunburst
}

// TakesOptionsAPI is TakesOptions for the API model ID that is sent (a setting of the user may name another).
func TakesOptionsAPI(apiModel string) bool {
	return apiModel == APIModelFlare || apiModel == APIModelSunburst
}

// SubscriptionRefusal is why the ChatGPT-subscription path does not take Options or a markup image: it is the unofficial Codex endpoint
// (chatgpt.com/backend-api/codex/responses), which OpenAI documents nowhere, and the Responses API's image_generation tool has no `markup`; whether
// that endpoint passes `complexity`, GIF or SVG output through is unconfirmed, so mam refuses rather than send them and show a plain PNG as if they were used.
const SubscriptionRefusal = "ChatGPT サブスク経由（Codex の非公式の経路）では、複雑さ・GIF・SVG・マークアップの指定は使えません。OpenAI の文書に、この経路がそれらを受け付けるという記載がないためです。API キー経由の GPT Image 2.6 を選んでください"

// Check checks the options alone: the values, and that the frame fields go with a GIF. The error is in Japanese (it is shown to the user).
func (o Options) Check() error {
	if o.Complexity != "" && !slices.Contains(Complexities(), o.Complexity) {
		return fmt.Errorf("複雑さ（complexity）は %v のどれかにしてください", Complexities())
	}
	if o.Format != "" && o.Format != FormatGIF && o.Format != FormatSVG {
		return fmt.Errorf("出力形式は GIF か SVG だけ指定できます（PNG は指定なし）")
	}
	if (o.FrameCount != 0 || o.FrameDurationMS != 0) && o.Format != FormatGIF {
		return fmt.Errorf("フレーム数とフレームの長さは GIF のときだけ指定できます")
	}
	if o.FrameCount != 0 && (o.FrameCount < MinFrameCount || o.FrameCount > MaxFrameCount) {
		return fmt.Errorf("フレーム数は %d〜%d にしてください", MinFrameCount, MaxFrameCount)
	}
	if o.FrameDurationMS != 0 && (o.FrameDurationMS < MinFrameDurationMS || o.FrameDurationMS > MaxFrameDurationMS) {
		return fmt.Errorf("フレームの長さは %d〜%d ミリ秒にしてください", MinFrameDurationMS, MaxFrameDurationMS)
	}
	return nil
}

// CheckRequest is Check together with the images the request carries: edit is true when the request has input images (the edits endpoint) and
// markup when it has a markup image. SVG output is for the generations endpoint only (the guide: "The image edit endpoint does not support SVG
// output"), and a markup image is the edits endpoint's field and is laid over the first input image.
func (o Options) CheckRequest(edit, markup bool) error {
	if err := o.Check(); err != nil {
		return err
	}
	if o.Format == FormatSVG && (edit || markup) {
		return fmt.Errorf("SVG 出力は新規の生成だけです（OpenAI の画像編集は SVG 出力に対応しません）。入力画像とマークアップを外してください")
	}
	if markup && !edit {
		return fmt.Errorf("マークアップは入力画像の 1 枚目に重ねる透明な PNG です。入力画像（編集対象）も付けてください")
	}
	return nil
}

// MIME is the type of the image a request with these options returns: image/gif, image/svg+xml, or "" for PNG.
func (o Options) MIME() string {
	switch o.Format {
	case FormatGIF:
		return "image/gif"
	case FormatSVG:
		return "image/svg+xml"
	}
	return ""
}

// JSONFields puts the options into the body of a generations request.
func (o Options) JSONFields(body map[string]any) {
	for k, v := range o.fields() {
		body[k] = v
	}
}

// FormFields lists the options as the text fields of an edits request (multipart), in a fixed order.
func (o Options) FormFields() [][2]string {
	fields := o.fields()
	var out [][2]string
	for _, k := range []string{"complexity", "output_format", "frame_count", "frame_duration_ms"} {
		switch v := fields[k].(type) {
		case string:
			out = append(out, [2]string{k, v})
		case int:
			out = append(out, [2]string{k, strconv.Itoa(v)})
		}
	}
	return out
}

func (o Options) fields() map[string]any {
	f := map[string]any{}
	if o.Complexity != "" {
		f["complexity"] = o.Complexity
	}
	if o.Format != "" {
		f["output_format"] = o.Format
	}
	if o.FrameCount != 0 {
		f["frame_count"] = o.FrameCount
	}
	if o.FrameDurationMS != 0 {
		f["frame_duration_ms"] = o.FrameDurationMS
	}
	return f
}

// CostKnown reports whether the token prices of this package make the cost of a call: an SVG is paid at the image model's rate for its reference image and at
// the authoring model's rate for the SVG itself ("Cost and latency", GPT Image 2.6 costs), and the authoring model's rate is not published as a price, so the
// cost of an SVG is unknown (ActualCost is nil), never the image part alone.
func (o Options) CostKnown() bool { return o.Format != FormatSVG }

// Caps is what GPT Image 2.6 takes, as the model list shows it (modelcatalog.Info.GPTImage) so that a screen and an MCP client offer only that.
type Caps struct {
	Complexities       []string `json:"complexities" jsonschema:"複雑さ（complexity）に指定できる値。指定しないと OpenAI の既定（high）"`
	Formats            []string `json:"formats" jsonschema:"PNG 以外の出力形式（gif: 生成と編集 / svg: 新規の生成だけ）。指定しないと PNG"`
	Markup             bool     `json:"markup" jsonschema:"マークアップ（入力画像の 1 枚目に重ねる透明な PNG）つきの編集に対応する"`
	MinFrameCount      int      `json:"min_frame_count" jsonschema:"GIF のフレーム数の下限"`
	MaxFrameCount      int      `json:"max_frame_count" jsonschema:"GIF のフレーム数の上限"`
	MinFrameDurationMS int      `json:"min_frame_duration_ms" jsonschema:"GIF の 1 フレームの長さ（ミリ秒）の下限"`
	MaxFrameDurationMS int      `json:"max_frame_duration_ms" jsonschema:"GIF の 1 フレームの長さ（ミリ秒）の上限"`
}

// CapsOf is the capabilities of the model: nil for any model that takes no Options (GPT Image 2, the subscription path, every other vendor).
func CapsOf(m domain.ModelName) *Caps {
	if !TakesOptions(m) {
		return nil
	}
	return &Caps{
		Complexities: Complexities(), Formats: []string{FormatGIF, FormatSVG}, Markup: true,
		MinFrameCount: MinFrameCount, MaxFrameCount: MaxFrameCount, MinFrameDurationMS: MinFrameDurationMS, MaxFrameDurationMS: MaxFrameDurationMS,
	}
}
