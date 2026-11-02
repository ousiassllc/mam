package redo

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/refform"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentestresult"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentesttier"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// ActionsOf are the buttons under the picture of an image result that has a file. inpaintURL is the screen that redraws part of it; upscale says
// the box of the enlargement is on the page. A result that is itself a redraw or an enlargement has no "again": its settings are not those of a
// plain generation (its source picture and mask are not part of the form).
func ActionsOf(d record.ResultDetail, inpaintURL, redrawFrom string, upscale bool) gentestresult.Actions {
	a := gentestresult.Actions{InpaintURL: inpaintURL, RedrawFrom: redrawFrom, Upscale: upscale}
	if d.Media != gentest.MediaImage || !d.HasFile {
		return gentestresult.Actions{}
	}
	if d.Inpaint == nil && d.Upscale == nil {
		a.SimilarURL = againURL(d.ID, false)
		a.ReplayURL = againURL(d.ID, true)
	}
	face := url.Values{gentesttier.FieldTier: {gentesttier.TierEasy}, gentesttier.FieldIntent: {"face"}, "ref": {refform.Value{Source: gentest.FromHistory, ID: d.ID}.String()}, "ref_role": {gentest.RoleFace}}
	a.FaceURL = paths.GenerationTest() + "?" + face.Encode()
	return a
}

func againURL(id string, sameSeed bool) string {
	q := url.Values{gentesttier.FieldTier: {gentesttier.TierDetail}, ParamAgain: {id}}
	if sameSeed {
		q.Set(ParamSameSeed, "1")
	}
	return paths.GenerationTest() + "?" + q.Encode()
}

// UsedOf are the settings the result was made with, for the folded panel: the model, the steps, the cfg, the sampler, the scheduler, the seed, the
// LoRA and the size (the values the sampling kept are those the model ran with, defaults included). A video result has no panel.
func UsedOf(d record.ResultDetail) gentestresult.Used {
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
