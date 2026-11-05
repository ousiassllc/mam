package redo

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestresult"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// ActionsOf are the buttons under the result that has a file. For an image: inpaintURL is the screen that redraws part of it; boxShown says the box of the
// enlargement is on the page. A result that is itself a redraw has its "again" in the redraw screen (its picture and mask are not part of the compose form),
// and one that is an enlargement opens the result it enlarged with the scale and the workflow chosen. A video has only the two of its box (大きくする・
// 滑らかにする; boxShown says it is on the page): none of the buttons that make a picture.
func ActionsOf(d record.ResultDetail, inpaintURL, redrawFrom string, boxShown bool) gentestresult.Actions {
	if d.Media == gentest.MediaVideo && d.HasFile {
		return gentestresult.Actions{VideoEnhance: boxShown}
	}
	a := gentestresult.Actions{InpaintURL: inpaintURL, RedrawFrom: redrawFrom, Upscale: boxShown}
	if d.Media != gentest.MediaImage || !d.HasFile {
		return gentestresult.Actions{}
	}
	switch {
	case d.Inpaint != nil:
		a.SimilarURL, a.ReplayURL = inpaintAgainURL(d.ID, false), inpaintAgainURL(d.ID, true)
	case d.FaceSwap != nil: // a swap has no sampling to repeat: its inputs are the picture and the face
	case d.Upscale != nil:
		a.UpscaleAgainURL = upscaleAgainURL(d.Upscale)
	default:
		a.SimilarURL = againURL(d.ID, false)
		a.ReplayURL = againURL(d.ID, true)
	}
	face := url.Values{gentesttier.FieldTier: {gentesttier.TierEasy}, gentesttier.FieldIntent: {"face"}, "ref": {refform.Value{Source: gentest.FromHistory, ID: d.ID}.String()}, "ref_role": {gentest.RoleFace}}
	a.FaceURL = paths.GenerationTest() + "?" + face.Encode()
	return a
}

// inpaintAgainURL opens the redraw screen with the redraw id made (the seed kept when sameSeed).
func inpaintAgainURL(id string, sameSeed bool) string {
	q := url.Values{ParamAgain: {id}}
	if sameSeed {
		q.Set(ParamSameSeed, "1")
	}
	return paths.GenerationTestInpaint() + "?" + q.Encode()
}

// Query names of the result page that opens the box of the enlargement with a scale and a workflow already chosen.
const (
	ParamUpscaleScale = "upscale_scale"
	ParamUpscaleModel = "upscale_model"
)

// upscaleAgainURL is the result the enlargement u came from, with the box of "大きくする" set to what u used. "" when the source is not known.
func upscaleAgainURL(u *record.Upscale) string {
	if u.SourceResultID == "" {
		return ""
	}
	q := url.Values{ParamUpscaleScale: {strconv.Itoa(u.Scale)}, ParamUpscaleModel: {u.Model}}
	return paths.GenerationTestPart("results", u.SourceResultID) + "?" + q.Encode() + gentestresult.UpscaleAnchor
}

func againURL(id string, sameSeed bool) string {
	q := url.Values{gentesttier.FieldTier: {gentesttier.TierDetail}, ParamAgain: {id}}
	if sameSeed {
		q.Set(ParamSameSeed, "1")
	}
	return paths.GenerationTest() + "?" + q.Encode()
}

// UsedOf are the settings the result was made with, for the folded panel: the model, the steps, the cfg, the sampler, the scheduler, the seed, the
// LoRA and the size (the values the sampling kept are those the model ran with, defaults included). A video result has no panel. takesSampling says
// the model takes steps, cfg, a sampler and a scheduler now: a result without them that it takes is one made before they were recorded.
func UsedOf(d record.ResultDetail, takesSampling bool) gentestresult.Used {
	if d.Media != gentest.MediaImage {
		return gentestresult.Used{}
	}
	rows := []gentestresult.Line{{Key: "モデル", Value: d.ModelLabel}}
	if s := d.Sampling; s != nil {
		if s.Steps != nil {
			rows = append(rows, gentestresult.Line{Key: "ステップ数", Value: strconv.Itoa(*s.Steps)})
		}
		if s.CFG != nil {
			rows = append(rows, gentestresult.Line{Key: "CFG", Value: strconv.FormatFloat(*s.CFG, 'f', -1, 64)})
		}
		if s.Sampler != "" {
			rows = append(rows, gentestresult.Line{Key: "サンプラー", Value: s.Sampler})
		}
		if s.Scheduler != "" {
			rows = append(rows, gentestresult.Line{Key: "スケジューラー", Value: s.Scheduler})
		}
	} else if takesSampling {
		rows = append(rows, gentestresult.Line{Key: "ステップ数・CFG・サンプラー", Value: "この結果には記録がありません（古い結果）"})
	} else {
		rows = append(rows, gentestresult.Line{Key: "ステップ数・CFG・サンプラー", Value: "このモデルは受け取りません（モデルの既定）"})
	}
	seed := "送っていません（このモデルは受け取りません）"
	if d.SeedSent && d.Seed != nil {
		seed = strconv.FormatInt(*d.Seed, 10)
	}
	lora := "なし"
	if d.LoRA != nil {
		lora = fmt.Sprintf("%s（強さ %.2f）", d.LoRA.Name, d.LoRA.Strength)
	}
	for _, k := range slices.Sorted(maps.Keys(d.Exposed)) {
		rows = append(rows, gentestresult.Line{Key: "画面に出した値 " + k, Value: fmt.Sprint(d.Exposed[k])})
	}
	return gentestresult.Used{Rows: append(rows, gentestresult.Line{Key: "シード", Value: seed}, gentestresult.Line{Key: "LoRA", Value: lora}, gentestresult.Line{Key: "サイズ", Value: SizeText(d.AspectRatio)})}
}

// SizeText is the size in the words of the buttons: 縦 / 横 / 正方形 with the aspect ratio.
func SizeText(aspect string) string {
	switch aspect {
	case "9:16":
		return "縦（9:16）"
	case "16:9":
		return "横（16:9）"
	case "1:1":
		return "正方形（1:1）"
	}
	return aspect
}
