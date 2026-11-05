package venhancescreen

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/videoenhancebox"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// FormFor is the "make the video larger / smoother" box of the result d: the models that can run now when d is a video with a file (a model without its
// key, or not installed, is not offered), and the link to the source when d is itself an enhancement. errMsg is the red line.
func FormFor(ctx context.Context, svc *gentest.Service, d record.ResultDetail, errMsg string) videoenhancebox.Form {
	f := videoenhancebox.Form{
		Error: errMsg, ResultID: d.ID, ModelsURL: paths.ModelManager(), SettingsURL: paths.Settings(),
		Upscale: videoenhancebox.Op{Name: videoenhancebox.OpUpscale}, Interpolate: videoenhancebox.Op{Name: videoenhancebox.OpInterpolate},
	}
	if v := d.VideoEnhance; v != nil {
		f.SourceURL = paths.GenerationTestPart("results", v.SourceResultID)
		f.SourceText = doneText(v.Op) + "結果です。元の結果を見る"
	}
	if d.Media != gentest.MediaVideo || !d.HasFile {
		return f
	}
	f.Show = true
	for _, m := range videoenhance.Enhancers(ctx, svc) {
		op := &f.Upscale
		if m.Op == string(venhance.OpInterpolate) {
			op = &f.Interpolate
		}
		switch {
		case m.Available && m.Paid:
			op.Topaz = append(op.Topaz, videoenhancebox.Option{ID: m.ID, Label: m.Label})
		case m.Available:
			op.Local = append(op.Local, videoenhancebox.Option{ID: m.ID, Label: m.Label})
		case m.Unavailable == modelcatalog.UnavailableNoKey:
			op.NoKey = true
		}
	}
	return f
}

// doneText says what an enhancement did, as a verb phrase.
func doneText(op string) string {
	if op == string(venhance.OpInterpolate) {
		return "コマを増やした"
	}
	return "大きくした"
}

// KVs are the rows of the detail of an enhancement: the source, what was done, the model and where it ran, the values, the sizes and rates, and for Topaz
// the credits and the request id (with the credit that Topaz asks to be shown).
func KVs(ctx context.Context, svc *gentest.Service, d record.ResultDetail) []view.KV {
	v := d.VideoEnhance
	if v == nil {
		return nil
	}
	provider := "この PC（ComfyUI）"
	if v.Provider == string(venhance.ProviderTopaz) {
		provider = "Topaz（有料クラウド）"
	}
	kv := []view.KV{
		{Key: "元の結果", Value: v.SourceResultID},
		{Key: "処理", Value: doneText(v.Op)},
		{Key: "使ったモデル", Value: fmt.Sprintf("%s（%s）", modelLabel(ctx, svc, v), provider)},
	}
	if v.Op == string(venhance.OpInterpolate) {
		kv = append(kv,
			view.KV{Key: "コマ数", Value: interpText(v)},
			view.KV{Key: "fps", Value: fmt.Sprintf("%g → %g（出力の予定）", v.SourceFPS, v.FPS)})
	} else {
		kv = append(kv,
			view.KV{Key: "倍率", Value: scaleText(v)},
			view.KV{Key: "大きさ", Value: fmt.Sprintf("%d × %d → %d × %d（出力の予定）", v.SourceWidth, v.SourceHeight, v.Width, v.Height)})
	}
	if t := v.Topaz; t != nil {
		kv = append(kv,
			view.KV{Key: "見積りのクレジット", Value: videoenhancebox.CreditsText(t.EstimatedCredits) + " クレジット（上限。確認した額）"},
			view.KV{Key: "Topaz のリクエスト ID", Value: t.RequestID},
			view.KV{Key: "出典", Value: orTopaz(t.Attribution)})
	}
	return kv
}

func orTopaz(s string) string {
	if s == "" {
		return venhance.TopazAttribution
	}
	return s
}

// modelLabel is the name of the model that made the result as the catalogue words it (the id when the model is gone).
func modelLabel(ctx context.Context, svc *gentest.Service, v *record.VideoEnhance) string {
	for _, m := range videoenhance.Enhancers(ctx, svc) {
		if m.ID == v.Model {
			return m.Label
		}
	}
	return v.Model
}

func scaleText(v *record.VideoEnhance) string {
	if v.TargetShortSide > 0 {
		return fmt.Sprintf("短い辺を %d px に", v.TargetShortSide)
	}
	return fmt.Sprintf("%g 倍", v.Scale)
}

func interpText(v *record.VideoEnhance) string {
	if v.TargetFPS > 0 {
		return fmt.Sprintf("%g fps にした", v.TargetFPS)
	}
	return fmt.Sprintf("%d 倍", v.Multiplier)
}
