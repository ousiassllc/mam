// Package gptform reads and writes the "GPT Image 2.6 の設定" of the generation test form (Issue #1096): the complexity, the output format (PNG, GIF, SVG)
// and the frames of a GIF, which only GPT Image 2.6 Flare and Sunburst (API key) take. The handlers of web/handler/gentest use it for the compose form, the
// estimate and the hidden fields of the confirmation, "この設定でもう一度" and the recipes. It never names the type of the options (the handler layer does
// not import the agent layer): the service's Request holds them, and what the models take comes from modelcatalog.Info.GPTImage.
//
// A value that is given is never left out: while a ticked model cannot take it the field stays in the form and posts, and the service refuses the request with the
// reason (a model that does not take them is not given them quietly).
package gptform

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestgpt"
)

// Fields are the settings as the form holds them: trimmed, "" when not given.
type Fields struct{ Complexity, Format, FrameCount, FrameDuration string }

// Read reads the fields of the form.
func Read(v url.Values) Fields {
	get := func(name string) string { return strings.TrimSpace(v.Get(name)) }
	return Fields{
		Complexity: get(gentestgpt.FieldComplexity), Format: get(gentestgpt.FieldFormat),
		FrameCount: get(gentestgpt.FieldFrameCount), FrameDuration: get(gentestgpt.FieldFrameDuration),
	}
}

// Given says a value that is not a frame was chosen: the complexity or an output format.
func (f Fields) Given() bool { return f.Complexity != "" || f.Format != "" }

// Apply puts the settings into the request. A number that is not one is refused here; whether the values are in range and go with the models and the images is
// the service's to say.
func (f Fields) Apply(r *gentest.Request) error {
	r.GPTImage.Complexity, r.GPTImage.Format = f.Complexity, f.Format
	if f.FrameCount != "" {
		n, err := strconv.Atoi(f.FrameCount)
		if err != nil {
			return gterr.Invalid("フレーム数は整数で指定してください")
		}
		r.GPTImage.FrameCount = n
	}
	if f.FrameDuration != "" {
		n, err := strconv.Atoi(f.FrameDuration)
		if err != nil {
			return gterr.Invalid("フレームの長さは整数（ミリ秒）で指定してください")
		}
		r.GPTImage.FrameDurationMS = n
	}
	return nil
}

// Of is the fields that hold the options a request or a result was made with: 0 for a count that was not given.
func Of(complexity, format string, frames, ms int) Fields {
	f := Fields{Complexity: complexity, Format: format}
	if frames != 0 {
		f.FrameCount = strconv.Itoa(frames)
	}
	if ms != 0 {
		f.FrameDuration = strconv.Itoa(ms)
	}
	return f
}

// OfResult is the settings the result was asked for (none when it used none).
func OfResult(d record.ResultView) Fields {
	if o := d.GPT; o != nil {
		return Of(o.Complexity, o.Format, o.FrameCount, o.FrameDurationMS)
	}
	return Fields{}
}

// Hidden carries the settings of a request to the confirmation, so that "実行" runs the same request.
func Hidden(r gentest.Request) []view.Hidden {
	var h []view.Hidden
	o := r.GPTImage
	if o.Complexity != "" {
		h = append(h, view.Hidden{Name: gentestgpt.FieldComplexity, Value: o.Complexity})
	}
	if o.Format != "" {
		h = append(h, view.Hidden{Name: gentestgpt.FieldFormat, Value: o.Format})
	}
	if o.FrameCount != 0 {
		h = append(h, view.Hidden{Name: gentestgpt.FieldFrameCount, Value: strconv.Itoa(o.FrameCount)})
	}
	if o.FrameDurationMS != 0 {
		h = append(h, view.Hidden{Name: gentestgpt.FieldFrameDuration, Value: strconv.Itoa(o.FrameDurationMS)})
	}
	return h
}

// OtherNote is the line for a ticked model that does not take the settings while some are given (a model of the family that is refused them says its own reason).
func OtherNote(label string) string {
	return label + ": GPT Image 2.6 の設定（複雑さ・出力形式）は受け取りません。指定なしに戻すか、このモデルを外してください"
}

// SectionOf is the section around the fields it holds. models are all the models of the form (the section is offered when one takes the settings or is
// refused them) and ticked the ones that are chosen now.
func SectionOf(f Fields, models []modelcatalog.Info, ticked []string) gentestgpt.Data {
	d := gentestgpt.Data{Complexity: f.Complexity, Format: f.Format, FrameCount: f.FrameCount, FrameDuration: f.FrameDuration}
	if i := slices.IndexFunc(models, func(m modelcatalog.Info) bool { return m.GPTImage != nil }); i >= 0 {
		c := models[i].GPTImage
		d.Complexities, d.Formats = c.Complexities, c.Formats
		d.MinFrames, d.MaxFrames, d.MinMS, d.MaxMS = c.MinFrameCount, c.MaxFrameCount, c.MinFrameDurationMS, c.MaxFrameDurationMS
	}
	capable, refused := false, false
	for _, m := range models {
		d.Offered = d.Offered || m.GPTImage != nil || m.GPTImageRefusal != ""
		if !m.Available || !slices.Contains(ticked, m.ID) {
			continue
		}
		switch {
		case m.GPTImage != nil:
			capable = true
		case m.GPTImageRefusal != "":
			refused = true
			d.Notes = append(d.Notes, m.Label+": "+m.GPTImageRefusal)
		case f.Given():
			d.Notes = append(d.Notes, OtherNote(m.Label))
		}
	}
	d.Fields = d.Offered && (capable || f.Given())
	d.Shown = d.Offered && (capable || refused || f.Given())
	return d
}

// CardOf is the card of a model of the form with what it says of these settings: a model that takes them says so, one of the family that is refused them
// says it does not (the reason is in its tooltip and, once it is ticked, in the section).
func CardOf(card modelcard.Card, m modelcatalog.Info) modelcard.Card {
	switch {
	case m.GPTImage != nil:
		card.Badges = append(card.Badges, modelcard.Badge{
			Tone: atoms.BadgeInfo, Label: "複雑さ・GIF・SVG・マークアップに対応",
			Attrs: templ.Attributes{"data-testid": "gt-model-gpt", "title": "GPT Image 2.6 の設定（複雑さ・GIF・SVG・マークアップ）を使えます。SVG は費用を見積れません"},
		})
	case m.GPTImageRefusal != "":
		card.Badges = append(card.Badges, modelcard.Badge{
			Tone: atoms.BadgeWarn, Label: "複雑さ・GIF・SVG・マークアップは使えません",
			Attrs: templ.Attributes{"data-testid": "gt-model-gpt-refused", "title": m.GPTImageRefusal},
		})
	}
	return card
}

// Line is a line of the settings of a result: what the detail and the folded panel of "使った設定" say.
type Line struct{ Key, Value string }

// Lines are the settings of GPT Image 2.6 that the result was asked for, in words: none for a result that used none (a PNG at OpenAI's default complexity).
func Lines(d record.ResultView) []Line {
	o := d.GPT
	if o == nil {
		return nil
	}
	var out []Line
	if o.Complexity != "" {
		out = append(out, Line{"複雑さ", o.Complexity})
	} else {
		out = append(out, Line{"複雑さ", "指定なし（OpenAI の既定: high）"})
	}
	out = append(out, Line{"出力形式", formatText(o.Format)})
	if o.Format == gentestgpt.FormatGIF {
		out = append(out, Line{"フレーム数", orDefault(o.FrameCount, "指定なし（OpenAI の既定: 9）", "")},
			Line{"フレームの長さ", orDefault(o.FrameDurationMS, "指定なし（OpenAI の既定: 200 ms）", " ms")})
	}
	return out
}

func orDefault(n int, def, unit string) string {
	if n == 0 {
		return def
	}
	return strconv.Itoa(n) + unit
}

func formatText(format string) string {
	switch format {
	case gentestgpt.FormatGIF:
		return "GIF"
	case gentestgpt.FormatSVG:
		return "SVG（費用は見積れません）"
	}
	return "PNG"
}

// Animated says a result file is a GIF or an SVG (what GPT Image 2.6 makes besides a PNG): a picture the pixel tools of the screen cannot take (the input
// images are PNG, JPEG and WebP).
func Animated(mime string) bool { return mime == "image/gif" || mime == "image/svg+xml" }

// NoPixelToolsNote is why a GIF or an SVG result is not offered as an input image, a redraw, an enlargement or a face swap.
const NoPixelToolsNote = "GIF・SVG の結果は、入力画像として使う・一部だけ描き直す・大きくする・顔を入れ替える・画像から似せる、には使えません（入力画像は PNG・JPEG・WebP です）"
